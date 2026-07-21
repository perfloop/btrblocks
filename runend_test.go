package btrblocks

import (
	"fmt"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

func buildPrimitiveRunEndTest[V Integer | Float](arr array.ArrayCore[V], cmp cmpFn[V]) (EncodedArray[V], error) {
	switch n := arr.Length(); {
	case n <= 1<<8:
		return encodePrimitiveRunEndAs[V, uint8](arr, cmp, testRawChild[V], testRawChild[uint8])
	case n <= 1<<16:
		return encodePrimitiveRunEndAs[V, uint16](arr, cmp, testRawChild[V], testRawChild[uint16])
	case n <= 1<<32:
		return encodePrimitiveRunEndAs[V, uint32](arr, cmp, testRawChild[V], testRawChild[uint32])
	default:
		return encodePrimitiveRunEndAs[V, uint64](arr, cmp, testRawChild[V], testRawChild[uint64])
	}
}

func buildStringRunEndTest(arr array.ArrayCore[string], cmp cmpFn[string]) (EncodedArray[string], error) {
	switch n := arr.Length(); {
	case n <= 1<<8:
		return encodeStringRunEndAs[uint8](arr, cmp, testRawStringChild, testRawChild[uint8])
	case n <= 1<<16:
		return encodeStringRunEndAs[uint16](arr, cmp, testRawStringChild, testRawChild[uint16])
	case n <= 1<<32:
		return encodeStringRunEndAs[uint32](arr, cmp, testRawStringChild, testRawChild[uint32])
	default:
		return encodeStringRunEndAs[uint64](arr, cmp, testRawStringChild, testRawChild[uint64])
	}
}

func TestRunEndRoundTrip(t *testing.T) {
	t.Run("uint32", func(t *testing.T) {
		values := []uint32{5, 5, 5, 5, 5, 7, 7, 7, 7, 7, 9, 9, 9, 9, 9}
		codec, err := buildPrimitiveRunEndTest(array.NewPrimitivesUnsafe(values), array.CmpIntegers[uint32])
		require.NoError(t, err)
		assertRoundTrip(t, codec, values)
	})

	t.Run("float64", func(t *testing.T) {
		values := make([]float64, 24)
		for i := range 8 {
			values[i] = 1.5
		}
		for i := 8; i < 16; i++ {
			values[i] = 2.5
		}
		for i := 16; i < 24; i++ {
			values[i] = 3.5
		}

		codec, err := buildPrimitiveRunEndTest(array.NewPrimitivesUnsafe(values), array.CmpFloatBits[float64])
		require.NoError(t, err)
		assertRoundTrip(t, codec, values)
	})

	t.Run("string", func(t *testing.T) {
		values := make([]string, 30)
		for i := range 10 {
			values[i] = "aaa"
		}
		for i := 10; i < 20; i++ {
			values[i] = "bbb"
		}
		for i := 20; i < 30; i++ {
			values[i] = "ccc"
		}

		codec, err := buildStringRunEndTest(mustStrings(t, values), array.CmpStrings[string])
		require.NoError(t, err)
		assertRoundTrip(t, codec, values)
	})

	t.Run("single_run", func(t *testing.T) {
		values := make([]uint32, 20)
		for i := range values {
			values[i] = 42
		}

		codec, err := buildPrimitiveRunEndTest(array.NewPrimitivesUnsafe(values), array.CmpIntegers[uint32])
		require.NoError(t, err)
		assertRoundTrip(t, codec, values)
	})
}

func TestRunEndEncoding(t *testing.T) {
	values := []uint32{5, 5, 5, 5, 5, 7, 7, 7, 7, 7, 9, 9, 9, 9, 9}
	codec, err := buildPrimitiveRunEndTest(array.NewPrimitivesUnsafe(values), array.CmpIntegers[uint32])
	require.NoError(t, err)
	require.Equal(t, CodecTypeRunEnd, codec.CodecType())
}

func TestRunEndValueAt(t *testing.T) {
	values := []uint32{5, 5, 5, 5, 5, 7, 7, 7, 7, 7, 9, 9, 9, 9, 9}
	codec, err := buildPrimitiveRunEndTest(array.NewPrimitivesUnsafe(values), array.CmpIntegers[uint32])
	require.NoError(t, err)

	require.Equal(t, uint32(5), codec.ValueAt(0))
	require.Equal(t, uint32(5), codec.ValueAt(4))
	require.Equal(t, uint32(7), codec.ValueAt(5))
	require.Equal(t, uint32(7), codec.ValueAt(9))
	require.Equal(t, uint32(9), codec.ValueAt(10))
	require.Equal(t, uint32(9), codec.ValueAt(14))
}

func makeRunEndInt64(n int) EncodedArray[int64] {
	values := make([]int64, n)
	vals := []int64{100, 200, 300, 400, 500}
	for i := range values {
		values[i] = vals[(i/10)%len(vals)]
	}
	codec, err := buildPrimitiveRunEndTest(array.NewPrimitivesUnsafe(values), array.CmpIntegers[int64])
	if err != nil {
		panic(err)
	}
	return codec
}

func BenchmarkRunEndCompress(b *testing.B) {
	for _, n := range benchSizes {
		values := make([]int64, n)
		vals := []int64{100, 200, 300, 400, 500}
		for i := range values {
			values[i] = vals[(i/10)%len(vals)]
		}
		arr := array.NewPrimitivesUnsafe(values)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := buildPrimitiveRunEndTest(arr, array.CmpIntegers[int64]); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkRunEndDecompress(b *testing.B) {
	for _, n := range benchSizes {
		codec := makeRunEndInt64(n)
		b.Run(fmt.Sprintf("n=%d/Decompress", n), func(b *testing.B) {
			benchDecompress(b, codec)
		})
		b.Run(fmt.Sprintf("n=%d/DecompressInto", n), func(b *testing.B) {
			benchDecompressInto(b, codec)
		})
	}
}

func FuzzRunEndUint8(f *testing.F) {
	f.Add([]byte{1, 1, 1, 1, 2, 2, 2, 2})
	f.Add([]byte{0, 0, 0, 0, 0, 0, 0, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 4 || len(data) > 1024 {
			return
		}
		values := make([]uint8, len(data))
		copy(values, data)

		codec, err := buildPrimitiveRunEndTest(array.NewPrimitivesUnsafe(values), array.CmpIntegers[uint8])
		if err != nil {
			return
		}

		assertRoundTrip(t, codec, values)
	})
}
