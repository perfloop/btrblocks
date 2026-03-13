package btrblocks

import (
	"testing"

	"github.com/axiomhq/btrblocks/array"
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
