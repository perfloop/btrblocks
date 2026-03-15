package btrblocks

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompressDispatchesDeterministicCodecs(t *testing.T) {
	t.Run("const integer", func(t *testing.T) {
		data := makeConstantCorpus(256, int64(9))
		codec := compress(data, defaultDepth)
		require.IsType(t, &ConstCodec[int64]{}, codec)
		assertCodecMetadata(t, codec, len(data), PTypeInt64, 0)
		assertCodecRoundTrip(t, codec, data)
	})

	t.Run("raw integer", func(t *testing.T) {
		data := makeRampInt64Corpus(256)
		codec := compress(data, defaultDepth)
		require.IsType(t, &RawCodec[int64]{}, codec)
		assertCodecMetadata(t, codec, len(data), PTypeInt64, 0)
		assertCodecRoundTrip(t, codec, data)
	})

	t.Run("const string", func(t *testing.T) {
		data := makeConstantCorpus(256, "zone-a")
		codec := compress(data, defaultDepth)
		require.IsType(t, &ConstCodec[string]{}, codec)
		assertCodecMetadata(t, codec, len(data), PTypeString, 0)
		assertCodecRoundTrip(t, codec, data)
	})

	t.Run("raw float", func(t *testing.T) {
		data := makeUniqueFloat64Corpus(256)
		codec := compress(data, defaultDepth)
		require.IsType(t, &RawCodec[float64]{}, codec)
		assertCodecMetadata(t, codec, len(data), PTypeFloat64, 0)
		assertCodecRoundTrip(t, codec, data)
	})
}

func TestCompressExportedFunctionsLargeCorpora(t *testing.T) {
	t.Run("default depth matches explicit", func(t *testing.T) {
		data := makeConstantCorpus(2048, uint64(17))
		codecDefault := Compress(data)
		codecExplicit := CompressWithDepth(data, defaultDepth)

		require.Equal(t, describeCodec(codecDefault), describeCodec(codecExplicit))

		assertCodecMetadata(t, codecDefault, len(data), PTypeUint64, 0)
		assertCodecRoundTrip(t, codecDefault, data)
		assertCodecMetadata(t, codecExplicit, len(data), PTypeUint64, 0)
		assertCodecRoundTrip(t, codecExplicit, data)
	})

	t.Run("large integer constant", func(t *testing.T) {
		data := makeConstantCorpus(largeCorpusSize, uint64(33))
		codec := Compress(data)
		assertCodecMetadata(t, codec, len(data), PTypeUint64, 0)
		assertCodecRoundTrip(t, codec, data)
	})

	t.Run("large integer unique", func(t *testing.T) {
		data := makeRampInt64Corpus(largeCorpusSize)
		codec := CompressWithDepth(data, defaultDepth)
		assertCodecMetadata(t, codec, len(data), PTypeInt64, 0)
		assertCodecRoundTrip(t, codec, data)
	})

	t.Run("large string patterned", func(t *testing.T) {
		data := makeLowCardinalityStringCorpus(largeCorpusSize, 8)
		codec := Compress(data)
		require.Contains(t, describeCodec(codec), "DictCodec[string,")
		assertCodecMetadata(t, codec, len(data), PTypeString, 2)
		assertCodecRoundTrip(t, codec, data)
	})
}

func FuzzCompressRoundTrip(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3})
	f.Add([]byte{9, 9, 1, 1, 0})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 2048 {
			data = data[:2048]
		}

		ints := make([]int64, len(data))
		floats := make([]float64, len(data))
		strings := make([]string, len(data))
		dict := []string{"aa", "bb", "cc", "dd", "ee", "ff", "gg", "hh"}

		for i, b := range data {
			ints[i] = int64(int8(b)) + int64(i%11)
			floats[i] = float64(ints[i])/3 + float64(i%5)/10
			strings[i] = dict[(int(b)+i)%len(dict)]
		}

		intCodec := CompressWithDepth(ints, defaultDepth)
		assertCodecMetadata(t, intCodec, len(ints), PTypeInt64, len(intCodec.Children()))
		assertCodecRoundTrip(t, intCodec, ints)

		floatCodec := Compress(floats)
		assertCodecMetadata(t, floatCodec, len(floats), PTypeFloat64, len(floatCodec.Children()))
		assertCodecRoundTrip(t, floatCodec, floats)

		stringCodec := compress(strings, defaultDepth)
		assertCodecMetadata(t, stringCodec, len(strings), PTypeString, len(stringCodec.Children()))
		assertCodecRoundTrip(t, stringCodec, strings)
	})
}

func BenchmarkCompressLarge(b *testing.B) {
	benchmarks := []struct {
		name string
		run  func(*testing.B)
	}{
		{
			name: "integer_const",
			run: func(b *testing.B) {
				data := makeConstantCorpus(largeCorpusSize, uint64(19))
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if codec := Compress(data); codec == nil {
						b.Fatal("Compress() returned nil")
					}
				}
			},
		},
		{
			name: "integer_unique",
			run: func(b *testing.B) {
				data := makeRampInt64Corpus(largeCorpusSize)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if codec := CompressWithDepth(data, defaultDepth); codec == nil {
						b.Fatal("CompressWithDepth() returned nil")
					}
				}
			},
		},
		{
			name: "string_patterned",
			run: func(b *testing.B) {
				data := makeLowCardinalityStringCorpus(largeCorpusSize, 8)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if codec := compress(data, defaultDepth); codec == nil {
						b.Fatal("compress() returned nil")
					}
				}
			},
		},
	}

	for _, benchmark := range benchmarks {
		b.Run(benchmark.name, benchmark.run)
	}
}
