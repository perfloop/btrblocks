package btrblocks

import (
	"fmt"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

type testDictionaryChildren[T Integer | Float] struct {
	testUnsignedChildren
}

func (testDictionaryChildren[T]) BuildValues(child array.ArrayCore[T]) (EncodedArray[T], error) {
	return testRawChild(child)
}

type testStringDictionaryChildren struct {
	testUnsignedChildren
}

func (testStringDictionaryChildren) BuildValues(child array.ArrayCore[string]) (EncodedArray[string], error) {
	return testRawStringChild(child)
}

func buildIntegerDictTest[T Integer](arr array.ArrayCore[T]) (EncodedArray[T], error) {
	return encodeIntegerDict(arr, nil, testDictionaryChildren[T]{})
}

func buildFloat64DictTest(arr array.ArrayCore[float64]) (EncodedArray[float64], error) {
	return encodeFloat64Dict(arr, nil, 0, testDictionaryChildren[float64]{})
}

func buildStringDictTest(arr array.ArrayCore[string]) (EncodedArray[string], error) {
	return encodeStringDict(arr, testStringDictionaryChildren{})
}

func TestDictRoundTrip(t *testing.T) {
	t.Run("int32", func(t *testing.T) {
		pattern := []int32{10, 20, 30}
		values := make([]int32, 100)
		for i := range values {
			values[i] = pattern[i%len(pattern)]
		}

		codec, err := buildIntegerDictTest(buildArray(values))
		require.NoError(t, err)
		require.Equal(t, CodecTypeDict, codec.CodecType())
		assertRoundTrip(t, codec, values)
	})

	t.Run("float64", func(t *testing.T) {
		pattern := []float64{1.1, 2.2, 3.3, 4.4, 5.5}
		values := make([]float64, 200)
		for i := range values {
			values[i] = pattern[i%len(pattern)]
		}

		codec, err := buildFloat64DictTest(buildArray(values))
		require.NoError(t, err)
		require.Equal(t, CodecTypeDict, codec.CodecType())
		assertRoundTrip(t, codec, values)
	})

	t.Run("string", func(t *testing.T) {
		pattern := []string{"alpha", "beta", "gamma", "delta"}
		values := make([]string, 100)
		for i := range values {
			values[i] = pattern[i%len(pattern)]
		}

		codec, err := buildStringDictTest(mustStrings(t, values))
		require.NoError(t, err)
		require.Equal(t, CodecTypeDict, codec.CodecType())
		assertRoundTrip(t, codec, values)
	})
}

func TestDictValueAt(t *testing.T) {
	values := []int32{10, 20, 30, 10, 20, 30}
	codec, err := buildIntegerDictTest(buildArray(values))
	require.NoError(t, err)

	for i, want := range values {
		require.Equal(t, want, codec.ValueAt(uint64(i)))
	}
}

func TestDictEncodingType(t *testing.T) {
	codec, err := buildIntegerDictTest(buildArray([]uint32{1, 2, 1, 2, 1, 2}))
	require.NoError(t, err)
	require.Equal(t, CodecTypeDict, codec.CodecType())
}

func TestDictSlicePreservesValues(t *testing.T) {
	values := []int32{10, 20, 30, 10, 20, 30, 10, 20}
	codec, err := buildIntegerDictTest(buildArray(values))
	require.NoError(t, err)
	require.Equal(t, CodecTypeDict, codec.CodecType())
	assertSliceRoundTrip(t, codec, 2, 6, values)
}

func TestDictionaryView(t *testing.T) {
	for _, distinct := range []int{4, 300} {
		t.Run(fmt.Sprintf("distinct=%d", distinct), func(t *testing.T) {
			values := make([]string, distinct*4)
			for i := range values {
				values[i] = fmt.Sprintf("value-%03d", i%distinct)
			}
			encoded, err := buildStringDictTest(mustStrings(t, values))
			require.NoError(t, err)

			loaded, err := LoadStrings(mustWriteEncodedArray(t, encoded))
			require.NoError(t, err)
			view, ok := asDictionary(loaded)
			require.True(t, ok)
			require.Equal(t, uint64(distinct), view.NumDictionaryValues())

			dictionary := make([]string, view.NumDictionaryValues())
			require.NoError(t, view.DecompressDictionaryInto(dictionary))
			decoded, err := Decompress(loaded)
			require.NoError(t, err)
			require.Equal(t, values, decoded)

			matchingValues := make([]bool, distinct)
			matchingValues[1] = true
			var matchingRows []uint64
			require.NoError(t, view.VisitMatchingOrdinals(matchingValues, func(offset uint64) {
				matchingRows = append(matchingRows, offset)
			}))
			for _, row := range matchingRows {
				require.Equal(t, "value-001", values[row])
			}
			require.Equal(t, 4, len(matchingRows))
		})
	}

	raw := encodeRaw(mustStrings(t, []string{"a", "b", "c"}))
	_, ok := asDictionary(raw)
	require.True(t, !ok)
}

func TestDictionaryViewVisitsBitpackedOrdinalsWithoutFallback(t *testing.T) {
	values := make([]float64, 65536)
	for i := range values {
		values[i] = float64(i % 1024)
	}
	encoded, err := buildFloat64DictTest(buildArray(values))
	require.NoError(t, err)
	loaded, err := LoadFloat64(mustWriteEncodedArray(t, encoded))
	require.NoError(t, err)
	view, ok := asDictionary(loaded)
	require.True(t, ok)

	dictionary := make([]float64, view.NumDictionaryValues())
	require.NoError(t, view.DecompressDictionaryInto(dictionary))
	matches := make([]bool, len(dictionary))
	for i, value := range dictionary {
		matches[i] = value == 512
	}
	var rows []uint64
	require.NoError(t, view.VisitMatchingOrdinals(matches, func(row uint64) {
		rows = append(rows, row)
	}))
	require.Equal(t, 64, len(rows))
	for _, row := range rows {
		require.Equal(t, float64(512), values[row])
	}

	rows = rows[:0]
	require.NoError(t, view.VisitMatchingOrdinals(matches, func(row uint64) {
		rows = append(rows, row)
	}))
	require.Equal(t, 64, len(rows))
	encodedDictionary, ok := loaded.(*dictArray[float64, uint16])
	require.True(t, ok)
	require.True(t, encodedDictionary.bsi != nil)
}

func TestDictionaryViewRejectsInvalidDirectOrdinal(t *testing.T) {
	values := encodeRaw(buildArray([]float64{1, 2}))
	ordinals := encodeRaw(buildArray([]uint8{0, 2}))
	dictionary := &dictArray[float64, uint8]{values: values, indices: ordinals}

	err := dictionary.VisitMatchingOrdinals([]bool{true, true}, func(uint64) {})
	require.ErrorContains(t, err, "dict ordinal 2 at position 1 >= values length 2")
}

func TestLoadRejectsOutOfRangeDictionaryOrdinal(t *testing.T) {
	encoded := &dictArray[uint32, uint8]{
		values:  newRawArray(buildArray([]uint32{10, 20})),
		indices: newRawArray(buildArray([]uint8{0, 2})),
	}
	data := mustWriteEncodedArray(t, encoded)

	_, err := LoadUnsigned[uint32](data)
	require.ErrorContains(t, err, "ordinal 2")
}

func TestDictValueAtPanicsOnOutOfRangeOrdinal(t *testing.T) {
	// ordinal 5 is out of range for a 2-value dictionary: ValueAt panics (like
	// Arrow's Value(i)) rather than silently returning a zero value. Callers that
	// must not panic use DecompressInto / VisitMatchingOrdinals, which error.
	d := &dictArray[uint32, uint8]{
		values:  newRawArray(buildArray([]uint32{10, 20})),
		indices: newRawArray(buildArray([]uint8{0, 5, 1})),
	}
	require.Equal(t, uint32(10), d.ValueAt(0))
	require.Panics(t, func() { d.ValueAt(1) })
}

func makeDictBenchData(n int) []uint32 {
	values := make([]uint32, n)
	pattern := []uint32{100, 200, 300, 400, 500, 600, 700, 800, 900, 1000}
	for i := range values {
		values[i] = pattern[i%len(pattern)]
	}
	return values
}

func BenchmarkDictCompress(b *testing.B) {
	for _, n := range benchSizes {
		values := makeDictBenchData(n)
		arr := buildArray(values)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := buildIntegerDictTest(arr); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkDictDecompress(b *testing.B) {
	for _, n := range benchSizes {
		values := makeDictBenchData(n)
		codec, err := buildIntegerDictTest(buildArray(values))
		if err != nil {
			b.Fatal(err)
		}
		b.Run(fmt.Sprintf("n=%d/Decompress", n), func(b *testing.B) {
			benchDecompress(b, codec)
		})
		b.Run(fmt.Sprintf("n=%d/DecompressInto", n), func(b *testing.B) {
			benchDecompressInto(b, codec)
		})
	}
}

func FuzzDictRoundTrip(f *testing.F) {
	f.Add([]byte{1, 0, 2, 0, 1, 0, 2, 0, 3, 0, 1, 0, 3, 0, 2, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 8 || len(data)%2 != 0 {
			return
		}
		n := min(len(data)/2, 512)
		values := make([]uint16, n)
		for i := range values {
			values[i] = uint16(data[2*i]) | uint16(data[2*i+1])<<8
		}
		distinct := make(map[uint16]struct{})
		for _, v := range values {
			distinct[v] = struct{}{}
		}
		if len(distinct) < 2 || len(distinct) >= len(values)/2 {
			return
		}

		codec, err := buildIntegerDictTest(buildArray(values))
		if err != nil {
			return
		}

		assertRoundTrip(t, codec, values)
	})
}
