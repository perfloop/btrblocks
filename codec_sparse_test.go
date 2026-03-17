package btrblocks

import (
	"bytes"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

// 1000 i32 values where 95% are 1_000_000
// (fill value) and 5% are varying (2_000_000 + (i*7) % 1000).
func TestSparseCodecDominantValue(t *testing.T) {
	data := make([]uint32, 1000)
	for i := range data {
		if i%20 == 0 {
			data[i] = 2_000_000 + uint32((i*7)%1000)
		} else {
			data[i] = 1_000_000
		}
	}

	codec, err := NewSparseIntegerCodec(array.NewPrimitivesUnsafe(data), defaultDepth, 0)
	require.NoError(t, err)

	assertCodecMetadata(t, codec, len(data), PTypeUint32, 2)
	assertCodecRoundTrip(t, codec, data)
}

// Since we don't have nulls, test with a dominant value and a few exceptions.
func TestSparseCodecSmallArray(t *testing.T) {
	data := []uint8{189, 189, 189, 189, 46}

	codec, err := NewSparseIntegerCodec(array.NewPrimitivesUnsafe(data), defaultDepth, 0)
	require.NoError(t, err)

	assertCodecMetadata(t, codec, len(data), PTypeUint8, 2)
	assertCodecRoundTrip(t, codec, data)

	// Verify children structure: values (non-fillers) and offsets.
	children := codec.Children()
	require.Len(t, children, 2)
	// 1 non-filler value (46 at position 4).
	require.Equal(t, uint64(1), requireChildCodecMetadata(t, children[0]).Length())
	require.Equal(t, uint64(1), requireChildCodecMetadata(t, children[1]).Length())
}

func TestSparseCodecRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		data []uint64
	}{
		{
			name: "95 percent dominant",
			data: func() []uint64 {
				d := make([]uint64, 100)
				for i := range d {
					d[i] = 42
				}
				d[7] = 100
				d[23] = 200
				d[55] = 300
				d[91] = 400
				d[99] = 500
				return d
			}(),
		},
		{
			name: "single exception",
			data: func() []uint64 {
				d := make([]uint64, 20)
				for i := range d {
					d[i] = 7
				}
				d[10] = 99
				return d
			}(),
		},
		{
			name: "all same",
			data: makeConstantCorpus(50, uint64(12345)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codec, err := NewSparseIntegerCodec(array.NewPrimitivesUnsafe(tt.data), defaultDepth, 0)
			require.NoError(t, err)

			assertCodecMetadata(t, codec, len(tt.data), PTypeUint64, 2)
			assertCodecRoundTrip(t, codec, tt.data)
		})
	}
}

func TestSparseCodecFloat(t *testing.T) {
	data := make([]float64, 100)
	for i := range data {
		data[i] = 3.14
	}
	data[5] = 2.71
	data[42] = 1.41
	data[99] = 0.0

	codec, err := NewSparseFloatCodec(array.NewPrimitivesUnsafe(data), defaultDepth, 0)
	require.NoError(t, err)

	assertCodecMetadata(t, codec, len(data), PTypeFloat64, 2)
	assertCodecRoundTrip(t, codec, data)
}

func TestSparseCodecString(t *testing.T) {
	data := make([]string, 50)
	for i := range data {
		data[i] = "hello"
	}
	data[3] = "world"
	data[27] = "foo"

	codec, err := NewSparseStringCodec(array.NewStrings(data), defaultDepth, 0)
	require.NoError(t, err)

	assertCodecMetadata(t, codec, len(data), PTypeString, 2)
	assertCodecRoundTrip(t, codec, data)
}

func TestSparseCodecErrors(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		_, err := NewSparseIntegerCodec(array.NewPrimitivesUnsafe([]uint64{}), defaultDepth, 0)
		require.ErrorIs(t, err, errDataEmpty)
	})

	t.Run("depth exhausted", func(t *testing.T) {
		data := makeConstantCorpus(10, uint64(5))
		_, err := NewSparseIntegerCodec(array.NewPrimitivesUnsafe(data), 0, 0)
		require.ErrorIs(t, err, errDepthExhausted)
	})
}

func TestSparseCodecWriteToReadRoundTrip(t *testing.T) {
	data := make([]uint32, 100)
	for i := range data {
		data[i] = 42
	}
	data[10] = 100
	data[50] = 200
	data[99] = 300

	codec, err := NewSparseIntegerCodec(array.NewPrimitivesUnsafe(data), defaultDepth, 0)
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
	require.Len(t, decoded.Children(), 2)

	for i, want := range data {
		got, err := decoded.ValueAt(uint64(i))
		require.NoError(t, err)
		require.Equal(t, want, got, "ValueAt(%d)", i)
	}
}

func TestSparseCodecLargeCorpus(t *testing.T) {
	data := make([]uint32, largeCorpusSize)
	for i := range data {
		if i%100 == 0 {
			data[i] = uint32(i)
		} else {
			data[i] = 42
		}
	}

	codec, err := NewSparseIntegerCodec(array.NewPrimitivesUnsafe(data), defaultDepth, 0)
	require.NoError(t, err)

	assertCodecMetadata(t, codec, len(data), PTypeUint32, 2)
	assertCodecRoundTrip(t, codec, data)
}

func TestNestedDecodeSparse(t *testing.T) {
	values := &spyCodec[uint32]{data: []uint32{100, 200}, pType: PTypeUint32}
	offsets := &spyCodec[uint8]{data: []uint8{2, 7}, pType: PTypeUint8}
	codec := &SparseCodec[uint32, uint8]{length: 10, filler: 42, values: values, offsets: offsets}
	dst := make([]uint32, 10)

	require.NoError(t, codec.Decode(dst))
	require.Equal(t, []uint32{42, 42, 100, 42, 42, 42, 42, 200, 42, 42}, dst)
	require.Equal(t, 1, values.decodeCalls)
	require.Equal(t, 1, offsets.decodeCalls)
}

func TestSparseCodecValueAtBinarySearch(t *testing.T) {
	values := &spyCodec[uint64]{data: []uint64{100, 200, 300}, pType: PTypeUint64}
	offsets := &spyCodec[uint8]{data: []uint8{3, 7, 15}, pType: PTypeUint8}
	codec := &SparseCodec[uint64, uint8]{length: 20, filler: 42, values: values, offsets: offsets}

	// Hit filler positions.
	for _, pos := range []uint64{0, 1, 2, 4, 5, 6, 8, 10, 16, 19} {
		got, err := codec.ValueAt(pos)
		require.NoError(t, err)
		require.Equal(t, uint64(42), got, "ValueAt(%d)", pos)
	}

	// Hit non-filler positions.
	got, err := codec.ValueAt(3)
	require.NoError(t, err)
	require.Equal(t, uint64(100), got)

	got, err = codec.ValueAt(7)
	require.NoError(t, err)
	require.Equal(t, uint64(200), got)

	got, err = codec.ValueAt(15)
	require.NoError(t, err)
	require.Equal(t, uint64(300), got)

	// Out of range.
	_, err = codec.ValueAt(20)
	require.ErrorIs(t, err, errOffsetOutOfRange)
}

func TestReadSparseCodecRejectsInvalidStructure(t *testing.T) {
	t.Run("wrong child count", func(t *testing.T) {
		var buf bytes.Buffer
		_, err := Header{
			Version:    1,
			Kind:       CodecTypeSparse,
			ElemType:   PTypeUint32,
			ChildCount: 1,
			Flags:      0,
			Length:     10,
			BodySize:   0,
		}.WriteTo(&buf)
		require.NoError(t, err)

		_, err = readCodec[uint32](bytes.NewReader(buf.Bytes()))
		require.ErrorContains(t, err, "sparse child count")
	})

	t.Run("zero length", func(t *testing.T) {
		var buf bytes.Buffer
		_, err := Header{
			Version:    1,
			Kind:       CodecTypeSparse,
			ElemType:   PTypeUint32,
			ChildCount: 2,
			Flags:      0,
			Length:     0,
			BodySize:   0,
		}.WriteTo(&buf)
		require.NoError(t, err)

		_, err = readCodec[uint32](bytes.NewReader(buf.Bytes()))
		require.ErrorContains(t, err, "sparse length")
	})
}

func FuzzSparseCodecRoundTrip(f *testing.F) {
	f.Add([]byte{42, 42, 42, 42, 10})
	f.Add([]byte{0, 0, 0, 0, 0})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 2 || len(data) > 512 {
			return
		}

		// Use first byte as the dominant value, sprinkle exceptions.
		filler := data[0]
		values := make([]uint8, len(data))
		for i := range values {
			values[i] = filler
		}
		for i := 1; i < len(data); i++ {
			if data[i] != filler {
				values[i] = data[i]
			}
		}

		codec, err := NewSparseIntegerCodec(array.NewPrimitivesUnsafe(values), defaultDepth, 0)
		require.NoError(t, err)

		assertCodecMetadata(t, codec, len(values), PTypeUint8, 2)
		assertCodecRoundTrip(t, codec, values)
	})
}

func BenchmarkSparseCodecBuildLarge(b *testing.B) {
	data := make([]uint32, largeCorpusSize)
	for i := range data {
		if i%100 == 0 {
			data[i] = uint32(i)
		} else {
			data[i] = 42
		}
	}

	benchmarkBuildLoop(b, "sparse", func(values []uint32) (Codec[uint32], error) {
		return NewSparseIntegerCodec(array.NewPrimitivesUnsafe(values), defaultDepth, 0)
	}, data)
}

func BenchmarkSparseCodecDecodeLarge(b *testing.B) {
	data := make([]uint32, largeCorpusSize)
	for i := range data {
		if i%100 == 0 {
			data[i] = uint32(i)
		} else {
			data[i] = 42
		}
	}

	codec, err := NewSparseIntegerCodec(array.NewPrimitivesUnsafe(data), defaultDepth, 0)
	if err != nil {
		b.Fatalf("NewSparseIntegerCodec() returned error: %v", err)
	}

	dst := make([]uint32, len(data))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = codec.Decode(dst)
	}
}

func BenchmarkSparseCodecValueAtLarge(b *testing.B) {
	data := make([]uint32, largeCorpusSize)
	for i := range data {
		if i%100 == 0 {
			data[i] = uint32(i)
		} else {
			data[i] = 42
		}
	}

	codec, err := NewSparseIntegerCodec(array.NewPrimitivesUnsafe(data), defaultDepth, 0)
	if err != nil {
		b.Fatalf("NewSparseIntegerCodec() returned error: %v", err)
	}
	benchmarkValueAtLoop(b, codec, len(data))
}

func BenchmarkSparseCodecWriteToLarge(b *testing.B) {
	data := make([]uint32, largeCorpusSize)
	for i := range data {
		if i%100 == 0 {
			data[i] = uint32(i)
		} else {
			data[i] = 42
		}
	}

	codec, err := NewSparseIntegerCodec(array.NewPrimitivesUnsafe(data), defaultDepth, 0)
	if err != nil {
		b.Fatalf("NewSparseIntegerCodec() returned error: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		var buf bytes.Buffer
		_, _ = codec.WriteTo(&buf)
	}
}
