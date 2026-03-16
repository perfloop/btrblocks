package btrblocks

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

// Vortex test_for_compressed: 1000 i32 values at 1_000_000 + ((i*37) % 100).
// FoR should be selected because the range (0-99) is much narrower than the
// absolute values (~1M), so subtracting min drastically reduces bit width.
func TestFoRCodecVortexHighBaseSmallDelta(t *testing.T) {
	data := make([]uint32, 1000)
	for i := range data {
		data[i] = 1_000_000 + uint32((i*37)%100)
	}

	codec, err := NewFoRCodec(array.NewPrimitivesUnsafe(data), defaultDepth, 0)
	require.NoError(t, err)

	assertCodecMetadata(t, codec, len(data), PTypeUint32, 1)
	assertCodecRoundTrip(t, codec, data)
}

func TestFoRCodecRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		data []uint64
	}{
		{
			name: "small offset range",
			data: []uint64{100, 103, 105, 101, 107, 100},
		},
		{
			name: "single element",
			data: []uint64{42},
		},
		{
			name: "all same value",
			data: []uint64{999, 999, 999, 999},
		},
		{
			name: "uint8 range",
			data: func() []uint64 {
				d := make([]uint64, 256)
				for i := range d {
					d[i] = 10000 + uint64(i)
				}
				return d
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codec, err := NewFoRCodec(array.NewPrimitivesUnsafe(tt.data), defaultDepth, 0)
			require.NoError(t, err)

			assertCodecMetadata(t, codec, len(tt.data), PTypeUint64, 1)
			assertCodecRoundTrip(t, codec, tt.data)
		})
	}
}

func TestFoRCodecWidthTypes(t *testing.T) {
	tests := []struct {
		name string
		data []uint8
	}{
		{
			name: "uint8",
			data: []uint8{200, 201, 202, 203},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codec, err := NewFoRCodec(array.NewPrimitivesUnsafe(tt.data), defaultDepth, 0)
			require.NoError(t, err)

			assertCodecMetadata(t, codec, len(tt.data), PTypeUint8, 1)
			assertCodecRoundTrip(t, codec, tt.data)
		})
	}

	t.Run("uint16", func(t *testing.T) {
		data := []uint16{50000, 50001, 50010, 50003}
		codec, err := NewFoRCodec(array.NewPrimitivesUnsafe(data), defaultDepth, 0)
		require.NoError(t, err)

		assertCodecMetadata(t, codec, len(data), PTypeUint16, 1)
		assertCodecRoundTrip(t, codec, data)
	})

	t.Run("uint32", func(t *testing.T) {
		data := []uint32{1 << 28, 1<<28 + 1, 1<<28 + 7}
		codec, err := NewFoRCodec(array.NewPrimitivesUnsafe(data), defaultDepth, 0)
		require.NoError(t, err)

		assertCodecMetadata(t, codec, len(data), PTypeUint32, 1)
		assertCodecRoundTrip(t, codec, data)
	})
}

func TestFoRCodecLargeCorpus(t *testing.T) {
	data := make([]uint32, largeCorpusSize)
	for i := range data {
		data[i] = 1_000_000 + uint32(i%1000)
	}

	codec, err := NewFoRCodec(array.NewPrimitivesUnsafe(data), defaultDepth, 0)
	require.NoError(t, err)

	assertCodecMetadata(t, codec, len(data), PTypeUint32, 1)
	assertCodecRoundTrip(t, codec, data)
}

func TestFoRCodecErrors(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		_, err := NewFoRCodec(array.NewPrimitivesUnsafe([]uint64{}), defaultDepth, 0)
		require.ErrorIs(t, err, errDataEmpty)
	})

	t.Run("depth exhausted", func(t *testing.T) {
		_, err := NewFoRCodec(array.NewPrimitivesUnsafe([]uint64{1, 2, 3}), 0, 0)
		require.ErrorIs(t, err, errDepthExhausted)
	})
}

func TestFoRCodecWriteToReadRoundTrip(t *testing.T) {
	data := []uint32{1_000_000, 1_000_005, 1_000_099, 1_000_001}

	codec, err := NewFoRCodec(array.NewPrimitivesUnsafe(data), defaultDepth, 0)
	require.NoError(t, err)

	var buf bytes.Buffer
	n, err := codec.WriteTo(&buf)
	require.NoError(t, err)
	require.Equal(t, int64(codec.BinarySize()), n)
	require.Equal(t, int(n), buf.Len())

	decoded, err := readCodec[uint32](bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	require.Equal(t, uint64(len(data)), decoded.Length())
	require.Equal(t, PTypeUint32, decoded.PType())
	require.Len(t, decoded.Children(), 1)

	for i, want := range data {
		got, err := decoded.ValueAt(uint64(i))
		require.NoError(t, err)
		require.Equal(t, want, got, "ValueAt(%d)", i)
	}
}

func TestReadFoRCodecRejectsMismatchedMetadata(t *testing.T) {
	codec, err := NewFoRCodec(array.NewPrimitivesUnsafe([]uint32{100, 105, 110}), defaultDepth, 0)
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	tests := []struct {
		name   string
		mutate func([]byte)
		want   string
	}{
		{
			name: "wrong child count",
			mutate: func(data []byte) {
				data[3] = 2 // ChildCount byte
			},
			want: "FoR child count",
		},
		{
			name: "wrong body size",
			mutate: func(data []byte) {
				binary.LittleEndian.PutUint64(data[16:24], 8)
			},
			want: "FoR body size",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := append([]byte(nil), buf.Bytes()...)
			tt.mutate(data)

			_, err := readCodec[uint32](bytes.NewReader(data))
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestNestedDecodeFoR(t *testing.T) {
	child := &spyCodec[uint32]{data: []uint32{0, 5, 99, 1}, pType: PTypeUint32}
	codec := &FoRCodec[uint32]{min: 1_000_000, data: child}
	dst := make([]uint32, 4)

	require.NoError(t, codec.Decode(dst))
	require.Equal(t, []uint32{1_000_000, 1_000_005, 1_000_099, 1_000_001}, dst)
	require.Equal(t, 1, child.decodeCalls)
}

func TestFoRCodecExcludesPreventsReentry(t *testing.T) {
	data := []uint64{1000, 1005, 1010, 1003}
	excl := codecExcludes(0).with(CodecTypeFoR)

	_, err := NewFoRCodec(array.NewPrimitivesUnsafe(data), defaultDepth, excl)
	// Should still succeed — excludes are for the child, not the codec itself.
	require.NoError(t, err)
}

func FuzzFoRCodecRoundTrip(f *testing.F) {
	f.Add([]byte{100, 103, 105, 101})
	f.Add([]byte{0, 0, 0, 0})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 || len(data) > 4096 {
			return
		}

		values := make([]uint16, len(data))
		for i, b := range data {
			values[i] = uint16(b) + 50000
		}

		codec, err := NewFoRCodec(array.NewPrimitivesUnsafe(values), defaultDepth, 0)
		require.NoError(t, err)

		assertCodecMetadata(t, codec, len(values), PTypeUint16, 1)
		assertCodecRoundTrip(t, codec, values)
	})
}

func BenchmarkFoRCodecBuildLarge(b *testing.B) {
	data := make([]uint32, largeCorpusSize)
	for i := range data {
		data[i] = 1_000_000 + uint32(i%1000)
	}

	benchmarkBuildLoop(b, "for", func(values []uint32) (Codec[uint32], error) {
		return NewFoRCodec(array.NewPrimitivesUnsafe(values), defaultDepth, 0)
	}, data)
}

func BenchmarkFoRCodecDecodeLarge(b *testing.B) {
	data := make([]uint32, largeCorpusSize)
	for i := range data {
		data[i] = 1_000_000 + uint32(i%1000)
	}

	codec, err := NewFoRCodec(array.NewPrimitivesUnsafe(data), defaultDepth, 0)
	if err != nil {
		b.Fatalf("NewFoRCodec() returned error: %v", err)
	}

	dst := make([]uint32, len(data))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = codec.Decode(dst)
	}
}

func BenchmarkFoRCodecValueAtLarge(b *testing.B) {
	data := make([]uint32, largeCorpusSize)
	for i := range data {
		data[i] = 1_000_000 + uint32(i%1000)
	}

	codec, err := NewFoRCodec(array.NewPrimitivesUnsafe(data), defaultDepth, 0)
	if err != nil {
		b.Fatalf("NewFoRCodec() returned error: %v", err)
	}
	benchmarkValueAtLoop(b, codec, len(data))
}

func BenchmarkFoRCodecWriteToLarge(b *testing.B) {
	data := make([]uint32, largeCorpusSize)
	for i := range data {
		data[i] = 1_000_000 + uint32(i%1000)
	}

	codec, err := NewFoRCodec(array.NewPrimitivesUnsafe(data), defaultDepth, 0)
	if err != nil {
		b.Fatalf("NewFoRCodec() returned error: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		var buf bytes.Buffer
		_, _ = codec.WriteTo(&buf)
	}
}
