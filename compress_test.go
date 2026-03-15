package btrblocks

import (
	"bytes"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

func TestReadAndDecompressRunendLargeCorpora(t *testing.T) {
	const (
		runLength   = 4096
		cardinality = 16
	)

	t.Run("int64", func(t *testing.T) {
		data := makeRunInt64CycleCorpus(largeCorpusSize, runLength, cardinality)
		codec, err := NewRunendIntegerCodec(data, defaultDepth)
		require.NoError(t, err)
		assertReadAndDecompressLargeCorpus(t, data, codec, &RunendCodec[int64, uint32]{})
	})

	t.Run("float64", func(t *testing.T) {
		data := makeRunFloat64CycleCorpus(largeCorpusSize, runLength, cardinality)
		codec, err := NewRunendFloatCodec(data, defaultDepth)
		require.NoError(t, err)
		assertReadAndDecompressLargeCorpus(t, data, codec, &RunendCodec[float64, uint32]{})
	})

	t.Run("string", func(t *testing.T) {
		data := makeRunStringCycleCorpus(largeCorpusSize, runLength, cardinality)
		codec, err := NewRunendStringCodec(data, defaultDepth)
		require.NoError(t, err)
		assertReadAndDecompressLargeCorpus(t, data, codec, &RunendCodec[string, uint32]{})
	})
}

func TestReadAndDecompressDictLargeCorpora(t *testing.T) {
	const cardinality = 16

	t.Run("int64", func(t *testing.T) {
		data := makeLowCardinalityInt64Corpus(largeCorpusSize, cardinality)
		codec, err := NewDictIntegerCodec(array.NewPrimitivesUnsafe(data), defaultDepth)
		require.NoError(t, err)
		assertReadAndDecompressLargeCorpus(t, data, codec, &DictCodec[int64, uint8]{})
	})

	t.Run("float64", func(t *testing.T) {
		data := makeLowCardinalityFloat64Corpus(largeCorpusSize, cardinality)
		codec, err := NewDictFloatCodec(array.NewPrimitivesUnsafe(data), defaultDepth)
		require.NoError(t, err)
		assertReadAndDecompressLargeCorpus(t, data, codec, &DictCodec[float64, uint8]{})
	})

	t.Run("string", func(t *testing.T) {
		data := makeLowCardinalityStringCorpus(largeCorpusSize, cardinality)
		codec, err := NewDictStringCodec(array.NewStrings(data), defaultDepth)
		require.NoError(t, err)
		assertReadAndDecompressLargeCorpus(t, data, codec, &DictCodec[string, uint8]{})
	})
}

func assertReadAndDecompressLargeCorpus[T Integer | Float | String](t *testing.T, data []T, codec Codec[T], wantType any) {
	t.Helper()

	var buf bytes.Buffer
	n, err := codec.WriteTo(&buf)
	require.NoError(t, err)
	require.Equal(t, int64(codec.BinarySize()), n)

	encoded := buf.Bytes()

	decoded, err := Read[T](bytes.NewReader(encoded))
	require.NoError(t, err)
	require.IsType(t, wantType, decoded)
	assertCodecMetadata(t, decoded, len(data), pTypeForType[T](), 2)

	for _, idx := range []int{0, 1, len(data) / 2, len(data) - 1} {
		got, err := decoded.ValueAt(uint64(idx))
		require.NoError(t, err, "ValueAt(%d)", idx)
		assertValueEqual(t, got, data[idx], idx)
	}

	values, err := Decompress[T](bytes.NewReader(encoded))
	require.NoError(t, err)
	require.Len(t, values, len(data))
	for i, want := range data {
		assertValueEqual(t, values[i], want, i)
	}
}

func makeRunInt64CycleCorpus(n, runLength, cardinality int) []int64 {
	if runLength < 1 {
		runLength = 1
	}
	if cardinality < 1 {
		cardinality = 1
	}
	out := make([]int64, n)
	offset := cardinality / 2
	for i := range out {
		out[i] = int64((i/runLength)%cardinality - offset)
	}
	return out
}

func makeRunFloat64CycleCorpus(n, runLength, cardinality int) []float64 {
	if runLength < 1 {
		runLength = 1
	}
	if cardinality < 1 {
		cardinality = 1
	}
	dict := make([]float64, cardinality)
	for i := range dict {
		dict[i] = float64(i) + float64(i%7)/8
	}
	out := make([]float64, n)
	for i := range out {
		out[i] = dict[(i/runLength)%cardinality]
	}
	return out
}

func makeRunStringCycleCorpus(n, runLength, cardinality int) []string {
	if runLength < 1 {
		runLength = 1
	}
	if cardinality < 1 {
		cardinality = 1
	}
	dict := make([]string, cardinality)
	for i := range dict {
		dict[i] = "run-value-" + string(rune('a'+(i%26))) + "-" + string(rune('0'+(i%10)))
	}
	out := make([]string, n)
	for i := range out {
		out[i] = dict[(i/runLength)%cardinality]
	}
	return out
}

func makeLowCardinalityInt64Corpus(n, cardinality int) []int64 {
	if cardinality < 1 {
		cardinality = 1
	}
	out := make([]int64, n)
	offset := cardinality / 2
	for i := range out {
		out[i] = int64((i*17+i/31)%cardinality - offset)
	}
	return out
}

func makeLowCardinalityFloat64Corpus(n, cardinality int) []float64 {
	if cardinality < 1 {
		cardinality = 1
	}
	dict := make([]float64, cardinality)
	for i := range dict {
		dict[i] = float64(i) + float64((i*3)%11)/16
	}
	out := make([]float64, n)
	for i := range out {
		out[i] = dict[(i*17+i/31)%cardinality]
	}
	return out
}
