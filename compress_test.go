package btrblocks

import (
	"bytes"
	"math"
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
		codec, err := NewRunendIntegerCodec(array.NewPrimitivesUnsafe(data), defaultDepth, 0)
		require.NoError(t, err)
		assertReadAndDecompressLargeCorpus(t, data, codec, &RunendCodec[int64, uint32]{})
	})

	t.Run("float64", func(t *testing.T) {
		data := makeRunFloat64CycleCorpus(largeCorpusSize, runLength, cardinality)
		codec, err := NewRunendFloatCodec(array.NewPrimitivesUnsafe(data), defaultDepth, 0)
		require.NoError(t, err)
		assertReadAndDecompressLargeCorpus(t, data, codec, &RunendCodec[float64, uint32]{})
	})

	t.Run("string", func(t *testing.T) {
		data := makeRunStringCycleCorpus(largeCorpusSize, runLength, cardinality)
		codec, err := NewRunendStringCodec(array.NewStrings(data), defaultDepth, 0)
		require.NoError(t, err)
		assertReadAndDecompressLargeCorpus(t, data, codec, &RunendCodec[string, uint32]{})
	})
}

func TestReadAndDecompressDictLargeCorpora(t *testing.T) {
	const cardinality = 16

	t.Run("int64", func(t *testing.T) {
		data := makeLowCardinalityInt64Corpus(largeCorpusSize, cardinality)
		codec, err := NewDictIntegerCodec(array.NewPrimitivesUnsafe(data), defaultDepth, 0)
		require.NoError(t, err)
		assertReadAndDecompressLargeCorpus(t, data, codec, &DictCodec[int64, uint8]{})
	})

	t.Run("float64", func(t *testing.T) {
		data := makeLowCardinalityFloat64Corpus(largeCorpusSize, cardinality)
		codec, err := NewDictFloatCodec(array.NewPrimitivesUnsafe(data), defaultDepth, 0)
		require.NoError(t, err)
		assertReadAndDecompressLargeCorpus(t, data, codec, &DictCodec[float64, uint8]{})
	})

	t.Run("string", func(t *testing.T) {
		data := makeLowCardinalityStringCorpus(largeCorpusSize, cardinality)
		codec, err := NewDictStringCodec(array.NewStrings(data), defaultDepth, 0)
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

// TestSchemeSelectionInteger verifies that CompressInteger selects the expected
// encoding for various data patterns, mirroring vortex-btrblocks
// scheme_selection_tests for integer compression.
func TestSchemeSelectionInteger(t *testing.T) {
	t.Run("constant", func(t *testing.T) {
		values := make([]int32, 100)
		for i := range values {
			values[i] = 42
		}
		codec := CompressInteger[int32](array.NewPrimitivesUnsafe(values), defaultDepth, 0)
		require.IsType(t, (*ConstCodec[int32])(nil), codec)
	})

	t.Run("bitpacking", func(t *testing.T) {
		values := make([]uint32, 1000)
		for i := range values {
			values[i] = uint32(i % 16)
		}
		codec := CompressUnsignedInteger[uint32](array.NewPrimitivesUnsafe(values), defaultDepth, 0)
		require.IsType(t, (*BitpackingCodec[uint32])(nil), codec)
	})

	t.Run("dict", func(t *testing.T) {
		// Six distinct large values with an interleaved (non-repeating) access
		// pattern so runs are length-1 and dict beats run-end.
		numbers := []int32{0, 123400, 617000, 1234000, 12340000, 37020000}
		values := make([]int32, 64000)
		for i := range values {
			values[i] = numbers[(i*7+3)%len(numbers)]
		}
		codec := CompressInteger[int32](array.NewPrimitivesUnsafe(values), defaultDepth, 0)
		_, ok := codec.(*DictCodec[int32, uint8])
		require.True(t, ok, "expected *DictCodec[int32, uint8], got %T", codec)
	})

	t.Run("runend", func(t *testing.T) {
		// 100 distinct large i32 values each repeated 10 times: long runs with
		// values too large for bitpacking to compress well, so run-end wins.
		values := make([]int32, 1000)
		for i := 0; i < 100; i++ {
			v := int32(1_000_000_000) + int32(i)
			for j := 0; j < 10; j++ {
				values[i*10+j] = v
			}
		}
		codec := CompressInteger[int32](array.NewPrimitivesUnsafe(values), defaultDepth, 0)
		_, ok := codec.(*RunendCodec[int32, uint16])
		require.True(t, ok, "expected *RunendCodec[int32, uint16], got %T", codec)
	})
}

// TestSchemeSelectionFloat verifies that CompressFloat selects the expected
// encoding for various data patterns, mirroring vortex-btrblocks
// scheme_selection_tests for float compression.
func TestSchemeSelectionFloat(t *testing.T) {
	t.Run("constant", func(t *testing.T) {
		values := make([]float64, 100)
		for i := range values {
			values[i] = 42.5
		}
		codec := CompressFloat[float64](array.NewPrimitivesUnsafe(values), defaultDepth, 0)
		require.IsType(t, (*ConstCodec[float64])(nil), codec)
	})

	t.Run("dict", func(t *testing.T) {
		// Five distinct float64 values interleaved so run-end loses.
		distinct := []float64{1.1, 2.2, 3.3, 4.4, 5.5}
		values := make([]float64, 1000)
		for i := range values {
			values[i] = distinct[i%len(distinct)]
		}
		codec := CompressFloat[float64](array.NewPrimitivesUnsafe(values), defaultDepth, 0)
		_, ok := codec.(*DictCodec[float64, uint8])
		require.True(t, ok, "expected *DictCodec[float64, uint8], got %T", codec)
	})
}

// TestSchemeSelectionString verifies that CompressString selects the expected
// encoding for various data patterns, mirroring vortex-btrblocks
// scheme_selection_tests for string compression.
func TestSchemeSelectionString(t *testing.T) {
	t.Run("constant", func(t *testing.T) {
		values := make([]string, 100)
		for i := range values {
			values[i] = "constant_value"
		}
		codec := CompressString(array.NewStrings(values), defaultDepth, 0)
		require.IsType(t, (*ConstCodec[string])(nil), codec)
	})

	t.Run("dict", func(t *testing.T) {
		// Three distinct strings interleaved so run-end loses.
		distinct := []string{"apple", "banana", "cherry"}
		values := make([]string, 1000)
		for i := range values {
			values[i] = distinct[i%len(distinct)]
		}
		codec := CompressString(array.NewStrings(values), defaultDepth, 0)
		_, ok := codec.(*DictCodec[string, uint8])
		require.True(t, ok, "expected *DictCodec[string, uint8], got %T", codec)
	})
}

// TestCompressEmptyArray verifies that compressing an empty array succeeds and
// produces a codec with length 0, mirroring vortex-btrblocks' test_empty.
func TestCompressEmptyArray(t *testing.T) {
	t.Run("integer", func(t *testing.T) {
		codec := CompressInteger[int32](array.NewPrimitivesUnsafe([]int32{}), defaultDepth, 0)
		require.NotNil(t, codec)
		require.Equal(t, uint64(0), codec.Length())
	})

	t.Run("float", func(t *testing.T) {
		codec := CompressFloat[float32](array.NewPrimitivesUnsafe([]float32{}), defaultDepth, 0)
		require.NotNil(t, codec)
		require.Equal(t, uint64(0), codec.Length())
	})
}

// TestCompressFloatCyclingValues verifies that 1024 float32 values cycling
// through 50 distinct entries are dict-compressed, mirroring vortex-btrblocks'
// test_compress.
func TestCompressFloatCyclingValues(t *testing.T) {
	values := make([]float32, 1024)
	for i := range values {
		values[i] = float32(i % 50)
	}
	codec := CompressFloat[float32](array.NewPrimitivesUnsafe(values), defaultDepth, 0)
	_, ok := codec.(*DictCodec[float32, uint8])
	require.True(t, ok, "expected *DictCodec[float32, uint8], got %T", codec)
	require.Equal(t, uint64(1024), codec.Length())
}

// TestCompressStringsDict verifies that low-cardinality interleaved strings are
// dict-compressed, mirroring vortex-btrblocks' test_strings and
// scheme_selection_tests/test_dict_compressed.
func TestCompressStringsDict(t *testing.T) {
	distinct := []string{"hello-world-1234", "hello-world-56789", "hello-world-99999"}
	values := make([]string, 3000)
	for i := range values {
		values[i] = distinct[i%len(distinct)]
	}
	codec := CompressString(array.NewStrings(values), defaultDepth, 0)
	_, ok := codec.(*DictCodec[string, uint8])
	require.True(t, ok, "expected *DictCodec[string, uint8], got %T", codec)
	require.Equal(t, uint64(3000), codec.Length())
}

// TestCompressIntegerDictEncodable verifies that integer arrays with a small
// number of distinct large values in short random-length runs are compressed
// with dict encoding, mirroring vortex-btrblocks' test_dict_encodable.
func TestCompressIntegerDictEncodable(t *testing.T) {
	numbers := []int32{0, 123400, 617000, 1234000, 12340000, 37020000}
	values := make([]int32, 0, 64000)
	// Deterministic pseudo-random runs of length 0–4 over the six values.
	for i := 0; len(values) < 64000; i++ {
		runLen := (i*7 + 3) % 5
		val := numbers[(i*13+5)%len(numbers)]
		for j := 0; j < runLen; j++ {
			values = append(values, val)
		}
	}
	values = values[:64000]
	codec := CompressInteger[int32](array.NewPrimitivesUnsafe(values), defaultDepth, 0)
	_, ok := codec.(*DictCodec[int32, uint8])
	require.True(t, ok, "expected *DictCodec[int32, uint8], got %T", codec)
}

// TestCompressNaNFloat verifies that compression and decompression of float
// arrays containing special IEEE 754 values (NaN, ±Inf, ±0) round-trips
// correctly by bit-pattern, mirroring vortex-btrblocks' test_sparse_compression.
func TestCompressNaNFloat(t *testing.T) {
	special := []float32{
		float32(math.NaN()),
		float32(math.Inf(1)),
		float32(math.Inf(-1)),
		0.0,
		float32(math.Copysign(0, -1)),
	}
	codec := CompressFloat[float32](array.NewPrimitivesUnsafe(special), defaultDepth, 0)
	require.NotNil(t, codec)
	require.Equal(t, uint64(len(special)), codec.Length())

	dst := make([]float32, len(special))
	require.NoError(t, codec.Decode(dst))
	for i, want := range special {
		require.Equal(t, math.Float32bits(want), math.Float32bits(dst[i]), "index %d", i)
	}
}
