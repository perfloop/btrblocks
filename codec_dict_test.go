package btrblocks

import (
	"math"
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

	codec, err := NewDictIntegerCodec(array.NewPrimitivesUnsafe(data), defaultDepth)
	require.NoError(t, err)

	assertCodecMetadata(t, codec, len(data), PTypeUint64, 2)
	assertCodecRoundTrip(t, codec, data)

	require.EqualValues(t, 16, codec.values.Length())
	require.Equal(t, uint64(len(data)), codec.indices.Length())
}

func TestDictCodecFloat64SpecialValuesRoundTrip(t *testing.T) {
	nan := math.Float64frombits(0x7ff8000000000001)
	data := []float64{
		nan,
		nan,
		math.Inf(1),
		math.Inf(1),
		math.Inf(-1),
		math.Inf(-1),
		1.5,
		1.5,
	}

	codec, err := NewDictFloatCodec(array.NewPrimitivesUnsafe(data), defaultDepth)
	require.NoError(t, err)

	assertCodecMetadata(t, codec, len(data), PTypeFloat64, 2)
	assertCodecRoundTrip(t, codec, data)

	require.EqualValues(t, countUniqueFloat64Bits(data), codec.values.Length())
	require.Equal(t, uint64(len(data)), codec.indices.Length())
}

func TestDictCodecFloat64DistinguishesBitPatterns(t *testing.T) {
	posZero := 0.0
	negZero := math.Copysign(0, -1)
	nanA := math.Float64frombits(0x7ff8000000000001)
	nanB := math.Float64frombits(0x7ff8000000000002)
	data := []float64{
		posZero,
		negZero,
		nanA,
		nanB,
		posZero,
		negZero,
		nanA,
		nanB,
	}

	codec, err := NewDictFloatCodec(array.NewPrimitivesUnsafe(data), defaultDepth)
	require.NoError(t, err)

	assertCodecMetadata(t, codec, len(data), PTypeFloat64, 2)
	assertCodecRoundTrip(t, codec, data)

	require.EqualValues(t, countUniqueFloat64Bits(data), codec.values.Length())
	require.Equal(t, uint64(len(data)), codec.indices.Length())
}

func TestCompressFloatSpecialValuesRoundTrip(t *testing.T) {
	patterns := []float64{
		math.Float64frombits(0x7ff8000000000001),
		math.Inf(1),
		math.Copysign(0, -1),
		42.5,
	}
	data := make([]float64, 512)
	for i := range data {
		data[i] = patterns[i%len(patterns)]
	}

	codec := CompressWithDepth(data, defaultDepth)
	assertCodecMetadata(t, codec, len(data), PTypeFloat64, len(codec.Children()))
	assertCodecRoundTrip(t, codec, data)
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

		codec, err := NewDictIntegerCodec(array.NewPrimitivesUnsafe(values), defaultDepth)
		require.NoError(t, err)

		assertCodecMetadata(t, codec, len(values), PTypeUint64, 2)
		assertCodecRoundTrip(t, codec, values)
	})
}

func BenchmarkDictCodecBuildLarge(b *testing.B) {
	data := makeLowCardinalityUint64Corpus(largeCorpusSize, 16)
	benchmarkBuildLoop(b, "dict", func(values []uint64) (Codec[uint64], error) {
		return NewDictIntegerCodec(array.NewPrimitivesUnsafe(values), defaultDepth)
	}, data)
}

func BenchmarkDictCodecValueAtLarge(b *testing.B) {
	data := makeLowCardinalityUint64Corpus(largeCorpusSize, 16)
	codec, err := NewDictIntegerCodec(array.NewPrimitivesUnsafe(data), defaultDepth)
	if err != nil {
		b.Fatalf("NewDictIntegerCodec() returned error: %v", err)
	}
	benchmarkValueAtLoop(b, codec, len(data))
}

func countUniqueFloat64Bits(data []float64) int {
	seen := make(map[uint64]struct{}, len(data))
	for _, value := range data {
		seen[math.Float64bits(value)] = struct{}{}
	}
	return len(seen)
}
