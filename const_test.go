package btrblocks

import (
	"bytes"
	"fmt"
	"math"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

func TestConstRoundTrip(t *testing.T) {
	t.Run("int32", func(t *testing.T) {
		values := []int32{42, 42, 42, 42, 42}
		codec, err := newConstIntegerArray(array.NewPrimitivesUnsafe(values))
		require.NoError(t, err)
		require.Equal(t, CodecTypeConst, codec.CodecType())
		require.Equal(t, uint64(len(values)), codec.Length())
		assertRoundTrip(t, codec, values)
	})
	t.Run("float64", func(t *testing.T) {
		values := []float64{3.14, 3.14, 3.14, 3.14}
		codec, err := newConstFloatArray(array.NewPrimitivesUnsafe(values))
		require.NoError(t, err)
		assertRoundTrip(t, codec, values)
	})
	t.Run("string", func(t *testing.T) {
		values := []string{"hello", "hello", "hello"}
		codec, err := newConstStringArray(mustStrings(t, values))
		require.NoError(t, err)
		assertRoundTrip(t, codec, values)
	})
	t.Run("float64 NaN", func(t *testing.T) {
		nan := math.NaN()
		values := []float64{nan, nan, nan}
		codec, err := newConstFloatArray(array.NewPrimitivesUnsafe(values))
		require.NoError(t, err)
		assertRoundTrip(t, codec, values)
	})
}

func TestConstRoundTripSmallTypes(t *testing.T) {
	t.Run("int8", func(t *testing.T) {
		values := []int8{-7, -7, -7}
		codec, err := newConstIntegerArray(array.NewPrimitivesUnsafe(values))
		require.NoError(t, err)
		assertRoundTrip(t, codec, values)
	})
	t.Run("int16", func(t *testing.T) {
		values := []int16{1234, 1234, 1234}
		codec, err := newConstIntegerArray(array.NewPrimitivesUnsafe(values))
		require.NoError(t, err)
		assertRoundTrip(t, codec, values)
	})
	t.Run("int32", func(t *testing.T) {
		values := []int32{-99999, -99999, -99999}
		codec, err := newConstIntegerArray(array.NewPrimitivesUnsafe(values))
		require.NoError(t, err)
		assertRoundTrip(t, codec, values)
	})
	t.Run("uint8", func(t *testing.T) {
		values := []uint8{200, 200, 200}
		codec, err := newConstIntegerArray(array.NewPrimitivesUnsafe(values))
		require.NoError(t, err)
		assertRoundTrip(t, codec, values)
	})
	t.Run("float32", func(t *testing.T) {
		values := []float32{3.14, 3.14, 3.14}
		codec, err := newConstFloatArray(array.NewPrimitivesUnsafe(values))
		require.NoError(t, err)
		assertRoundTrip(t, codec, values)
	})
}

func TestConstNonConstantDataReturnsError(t *testing.T) {
	_, err := newConstIntegerArray(array.NewPrimitivesUnsafe([]int32{1, 2, 1}))
	require.ErrorIs(t, err, errValueNotConstant)

	_, err = newConstFloatArray(array.NewPrimitivesUnsafe([]float64{1.0, 2.0}))
	require.ErrorIs(t, err, errValueNotConstant)

	_, err = newConstStringArray(mustStrings(t, []string{"a", "b"}))
	require.ErrorIs(t, err, errValueNotConstant)
}

func TestConstEmptyArrayReturnsError(t *testing.T) {
	_, err := newConstIntegerArray(array.NewPrimitivesUnsafe([]int32{}))
	require.ErrorIs(t, err, errDataEmpty)
}

func TestConstSlice(t *testing.T) {
	values := []uint64{99, 99, 99, 99, 99}
	codec, err := newConstIntegerArray(array.NewPrimitivesUnsafe(values))
	require.NoError(t, err)

	sliced, err := codec.Slice(1, 4)
	require.NoError(t, err)
	require.Equal(t, CodecTypeConst, sliced.CodecType())

	assertSliceRoundTrip(t, codec, 1, 4, values)
}

func BenchmarkConst(b *testing.B) {
	for _, n := range benchSizes {
		data := make([]uint64, n)
		for i := range data {
			data[i] = 42
		}

		b.Run(fmt.Sprintf("compress/%d", n), func(b *testing.B) {
			arr := array.NewPrimitivesUnsafe(data)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				codec, err := newConstIntegerArray(arr)
				if err != nil {
					b.Fatal(err)
				}
				_ = mustWriteEncodedArray(b, codec)
			}
		})

		b.Run(fmt.Sprintf("decompress/%d", n), func(b *testing.B) {
			arr := array.NewPrimitivesUnsafe(data)
			codec, err := newConstIntegerArray(arr)
			if err != nil {
				b.Fatal(err)
			}
			encoded := mustWriteEncodedArray(b, codec)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				readBack, err := LoadUnsigned[uint64](encoded)
				if err != nil {
					b.Fatal(err)
				}
				_, err = Decompress(readBack)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func FuzzConstUint8Roundtrip(f *testing.F) {
	f.Add([]byte{7, 7, 7, 7})
	f.Add([]byte{0})
	f.Add([]byte{255, 255})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			return
		}
		for i := 1; i < len(data); i++ {
			if data[i] != data[0] {
				return
			}
		}
		codec, err := newConstIntegerArray(array.NewPrimitivesUnsafe(data))
		require.NoError(t, err)
		assertRoundTrip(t, codec, data)
	})
}

func makeConstUint32(n int) EncodedArray[uint32] {
	values := make([]uint32, n)
	for i := range values {
		values[i] = 42
	}
	codec, err := newConstIntegerArray(array.NewPrimitivesUnsafe(values))
	if err != nil {
		panic(err)
	}
	return codec
}

func makeConstString(n int) EncodedArray[string] {
	values := make([]string, n)
	for i := range values {
		values[i] = "hello world"
	}
	arr, err := array.NewStrings(values)
	if err != nil {
		panic(err)
	}
	codec, err := newConstStringArray(arr)
	if err != nil {
		panic(err)
	}
	return codec
}

func BenchmarkConstDecompressInto_1K(b *testing.B)  { benchDecompressInto(b, makeConstUint32(1_000)) }
func BenchmarkConstDecompressInto_10K(b *testing.B) { benchDecompressInto(b, makeConstUint32(10_000)) }
func BenchmarkConstDecompressInto_100K(b *testing.B) {
	benchDecompressInto(b, makeConstUint32(100_000))
}
func BenchmarkConstDecompressInto_1M(b *testing.B) {
	benchDecompressInto(b, makeConstUint32(1_000_000))
}

func BenchmarkConstStringDecompressInto_10K(b *testing.B) {
	benchDecompressInto(b, makeConstString(10_000))
}
func BenchmarkConstStringDecompressInto_100K(b *testing.B) {
	benchDecompressInto(b, makeConstString(100_000))
}

func BenchmarkWriteConstElementArray(b *testing.B) {
	values := make([]uint64, 10_000)
	for i := range values {
		values[i] = 42
	}
	codec, err := newConstIntegerArray(array.NewPrimitivesUnsafe(values))
	if err != nil {
		b.Fatal(err)
	}

	var buf bytes.Buffer
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		buf.Reset()
		if _, err := codec.WriteTo(&buf); err != nil {
			b.Fatal(err)
		}
	}
}
