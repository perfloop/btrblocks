package btrblocks

import (
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

func TestDictCodecStringRoundTrip(t *testing.T) {
	data := []string{"east", "west", "east", "north", "west", "east", "south", "north"}

	codec, err := NewDictStringCodec(array.NewStrings(data), defaultDepth)
	require.NoError(t, err)

	assertCodecMetadata(t, codec, len(data), PTypeString, 2)
	assertCodecRoundTrip(t, codec, data)

	require.EqualValues(t, 4, codec.values.Length())
	require.Equal(t, uint64(len(data)), codec.indices.Length())
}

func TestDictCodecLargeCorpus(t *testing.T) {
	data := makeLowCardinalityUint64Corpus(largeCorpusSize, 16)

	codec, err := NewDictIntegerCodec(array.NewPrimitivesUnsafe[uint64](data), defaultDepth)
	require.NoError(t, err)

	assertCodecMetadata(t, codec, len(data), PTypeUnsignedInteger, 2)
	assertCodecRoundTrip(t, codec, data)

	require.EqualValues(t, 16, codec.values.Length())
	require.Equal(t, uint64(len(data)), codec.indices.Length())
}

func FuzzDictCodecRoundTrip(f *testing.F) {
	f.Add([]byte{0, 1, 0, 2, 0, 3})
	f.Add([]byte{9, 9, 9, 1, 1, 1})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 2048 {
			data = data[:2048]
		}

		values := make([]uint64, len(data))
		for i, b := range data {
			values[i] = uint64((int(b) + i) % 32)
		}

		codec, err := NewDictIntegerCodec(array.NewPrimitivesUnsafe[uint64](values), defaultDepth)
		require.NoError(t, err)

		assertCodecMetadata(t, codec, len(values), PTypeUnsignedInteger, 2)
		assertCodecRoundTrip(t, codec, values)
	})
}

func BenchmarkDictCodecBuildLarge(b *testing.B) {
	data := makeLowCardinalityUint64Corpus(largeCorpusSize, 16)
	benchmarkBuildLoop(b, "dict", func(values []uint64) (Codec[uint64], error) {
		return NewDictIntegerCodec(array.NewPrimitivesUnsafe[uint64](values), defaultDepth)
	}, data)
}

func BenchmarkDictCodecValueAtLarge(b *testing.B) {
	data := makeLowCardinalityUint64Corpus(largeCorpusSize, 16)
	codec, err := NewDictIntegerCodec(array.NewPrimitivesUnsafe[uint64](data), defaultDepth)
	if err != nil {
		b.Fatalf("NewDictIntegerCodec() returned error: %v", err)
	}
	benchmarkValueAtLoop(b, codec, len(data))
}
