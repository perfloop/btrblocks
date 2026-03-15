package btrblocks

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

func TestRawCodecStringRoundTrip(t *testing.T) {
	data := []string{"alpha", "beta", "", "delta", "beta"}
	codec := NewRawCodec(array.NewStrings(data))

	assertCodecMetadata(t, codec, len(data), PTypeString, 0)
	assertCodecRoundTrip(t, codec, data)
}

func TestRawCodecLargeCorpus(t *testing.T) {
	data := makeRampInt64Corpus(largeCorpusSize)
	codec := NewRawCodec(array.NewPrimitivesUnsafe[int64](data))

	assertCodecMetadata(t, codec, len(data), PTypeInt64, 0)
	assertCodecRoundTrip(t, codec, data)
}

func FuzzRawCodecRoundTrip(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3})
	f.Add([]byte{255, 0, 255, 0})

	f.Fuzz(func(t *testing.T, data []byte) {
		values := make([]uint16, len(data))
		for i, b := range data {
			values[i] = uint16(b) | uint16(i&7)<<8
		}

		codec := NewRawCodec(array.NewPrimitivesUnsafe[uint16](values))
		assertCodecMetadata(t, codec, len(values), PTypeUint16, 0)
		assertCodecRoundTrip(t, codec, values)
	})
}

func BenchmarkRawCodecBuildLarge(b *testing.B) {
	data := makeRampInt64Corpus(largeCorpusSize)
	benchmarkBuildLoop(b, "raw", func(values []int64) (Codec[int64], error) {
		return NewRawCodec(array.NewPrimitivesUnsafe[int64](values)), nil
	}, data)
}

func BenchmarkRawCodecValueAtLarge(b *testing.B) {
	data := makeRampInt64Corpus(largeCorpusSize)
	codec := NewRawCodec(array.NewPrimitivesUnsafe[int64](data))
	benchmarkValueAtLoop(b, codec, len(data))
}

func TestReadCodecNestedRawPrimitiveChildren(t *testing.T) {
	codec := &DictCodec[uint64, uint8]{
		values:  NewRawCodec(array.NewPrimitivesUnsafe([]uint64{11, 22})),
		indices: NewRawCodec(array.NewPrimitivesUnsafe([]uint8{0, 1, 0, 1})),
	}

	var buf bytes.Buffer
	_, err := codec.WriteTo(&buf)
	require.NoError(t, err)

	decoded, err := readCodec[uint64](bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	assertCodecMetadata(t, decoded, 4, PTypeUint64, 2)
	assertCodecRoundTrip(t, decoded, []uint64{11, 22, 11, 22})
}

func TestReadCodecNestedRawStringChildren(t *testing.T) {
	codec := &DictCodec[string, uint8]{
		values:  NewRawCodec(array.NewStrings([]string{"alpha", "beta"})),
		indices: NewRawCodec(array.NewPrimitivesUnsafe([]uint8{0, 1, 0})),
	}

	var buf bytes.Buffer
	_, err := codec.WriteTo(&buf)
	require.NoError(t, err)

	decoded, err := readCodec[string](bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	assertCodecMetadata(t, decoded, 3, PTypeString, 2)
	assertCodecRoundTrip(t, decoded, []string{"alpha", "beta", "alpha"})
}

func TestReadCodecNestedRawRunendChildren(t *testing.T) {
	codec := &RunendCodec[uint64, uint8]{
		length: 8,
		runs:   NewRawCodec(array.NewPrimitivesUnsafe([]uint64{5, 8, 13})),
		ends:   NewRawCodec(array.NewPrimitivesUnsafe([]uint8{3, 5})),
	}

	var buf bytes.Buffer
	_, err := codec.WriteTo(&buf)
	require.NoError(t, err)

	decoded, err := readCodec[uint64](bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	assertCodecMetadata(t, decoded, 8, PTypeUint64, 2)
	assertCodecRoundTrip(t, decoded, []uint64{5, 5, 5, 8, 8, 13, 13, 13})
}

func TestReadRawCodecZeroLengthRoundTrip(t *testing.T) {
	codec := NewRawCodec(array.NewPrimitivesUnsafe([]uint64{}))

	var buf bytes.Buffer
	_, err := codec.WriteTo(&buf)
	require.NoError(t, err)

	decoded, err := readCodec[uint64](bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	assertCodecMetadata(t, decoded, 0, PTypeUint64, 0)
}

func TestReadCodecRejectsUnsupportedHeader(t *testing.T) {
	tests := []struct {
		name   string
		header Header
		want   string
	}{
		{
			name:   "version",
			header: Header{Version: 2, Kind: CodecTypeRaw, ElemType: PTypeUint64, ChildCount: 0, Flags: 0, Length: 0, BodySize: 0},
			want:   "version",
		},
		{
			name:   "flags",
			header: Header{Version: 1, Kind: CodecTypeRaw, ElemType: PTypeUint64, ChildCount: 0, Flags: 1, Length: 0, BodySize: 0},
			want:   "flags",
		},
		{
			name:   "element type",
			header: Header{Version: 1, Kind: CodecTypeRaw, ElemType: PTypeUint32, ChildCount: 0, Flags: 0, Length: 0, BodySize: 0},
			want:   "element type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			_, err := tt.header.WriteTo(&buf)
			require.NoError(t, err)

			_, err = readCodec[uint64](bytes.NewReader(buf.Bytes()))
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestReadRawCodecRejectsMismatchedOuterMetadata(t *testing.T) {
	codec := NewRawCodec(array.NewPrimitivesUnsafe([]uint64{7, 11}))

	var buf bytes.Buffer
	_, err := codec.WriteTo(&buf)
	require.NoError(t, err)

	tests := []struct {
		name   string
		mutate func([]byte)
		want   string
	}{
		{
			name: "length",
			mutate: func(data []byte) {
				binary.LittleEndian.PutUint64(data[8:16], 3)
			},
			want: "raw length",
		},
		{
			name: "body size",
			mutate: func(data []byte) {
				binary.LittleEndian.PutUint64(data[16:24], 1)
			},
			want: "raw body size",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := append([]byte(nil), buf.Bytes()...)
			tt.mutate(data)

			_, err := readCodec[uint64](bytes.NewReader(data))
			require.ErrorContains(t, err, tt.want)
		})
	}
}
