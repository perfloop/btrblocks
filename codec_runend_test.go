package btrblocks

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

func TestRunEndRoundtripUint32(t *testing.T) {
	values := []uint32{5, 5, 5, 5, 5, 7, 7, 7, 7, 7, 9, 9, 9, 9, 9}
	codec, err := buildRunEndArray(array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}), cmpIntegers[uint32])
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	decoded, err := Read[uint32](&buf)
	require.NoError(t, err)

	got, err := Decompress(decoded)
	require.NoError(t, err)
	require.Equal(t, values, got)
}

func TestRunEndRoundtripFloat64(t *testing.T) {
	values := make([]float64, 24)
	for i := 0; i < 8; i++ {
		values[i] = 1.5
	}
	for i := 8; i < 16; i++ {
		values[i] = 2.5
	}
	for i := 16; i < 24; i++ {
		values[i] = 3.5
	}

	codec, err := buildRunEndArray(array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}), cmpFloats[float64])
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	decoded, err := Read[float64](&buf)
	require.NoError(t, err)

	got, err := Decompress(decoded)
	require.NoError(t, err)
	require.Equal(t, values, got)
}

func TestRunEndRoundtripString(t *testing.T) {
	values := make([]string, 30)
	for i := 0; i < 10; i++ {
		values[i] = "aaa"
	}
	for i := 10; i < 20; i++ {
		values[i] = "bbb"
	}
	for i := 20; i < 30; i++ {
		values[i] = "ccc"
	}

	codec, err := buildRunEndArray(array.NewStrings(values), newPlanContext(Options{MaxDepth: 3}), cmpStrings[string])
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	decoded, err := Read[string](&buf)
	require.NoError(t, err)

	got, err := Decompress(decoded)
	require.NoError(t, err)
	require.Equal(t, values, got)
}

func TestRunEndEncoding(t *testing.T) {
	values := []uint32{5, 5, 5, 5, 5, 7, 7, 7, 7, 7, 9, 9, 9, 9, 9}
	codec, err := buildRunEndArray(array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}), cmpIntegers[uint32])
	require.NoError(t, err)
	require.Equal(t, CodecTypeRunEnd, codec.Encoding())
}

func TestRunEndValueAt(t *testing.T) {
	values := []uint32{5, 5, 5, 5, 5, 7, 7, 7, 7, 7, 9, 9, 9, 9, 9}
	codec, err := buildRunEndArray(array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}), cmpIntegers[uint32])
	require.NoError(t, err)

	require.Equal(t, uint32(5), codec.ValueAt(0))
	require.Equal(t, uint32(5), codec.ValueAt(4))
	require.Equal(t, uint32(7), codec.ValueAt(5))
	require.Equal(t, uint32(7), codec.ValueAt(9))
	require.Equal(t, uint32(9), codec.ValueAt(10))
	require.Equal(t, uint32(9), codec.ValueAt(14))
}

func TestRunEndSingleRun(t *testing.T) {
	values := make([]uint32, 20)
	for i := range values {
		values[i] = 42
	}

	codec, err := buildRunEndArray(array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}), cmpIntegers[uint32])
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	decoded, err := Read[uint32](&buf)
	require.NoError(t, err)

	got, err := Decompress(decoded)
	require.NoError(t, err)
	require.Equal(t, values, got)
}

func makeRunEndInt64(n int) EncodedArray[int64] {
	values := make([]int64, n)
	vals := []int64{100, 200, 300, 400, 500}
	for i := range values {
		values[i] = vals[(i/10)%len(vals)]
	}
	codec, err := Compress(array.NewPrimitivesUnsafe(values), Options{})
	if err != nil {
		panic(err)
	}
	return codec
}

func BenchmarkRunEndCompress(b *testing.B) {
	for _, n := range []int{1_000, 10_000, 100_000, 1_000_000} {
		values := make([]int64, n)
		vals := []int64{100, 200, 300, 400, 500}
		for i := range values {
			values[i] = vals[(i/10)%len(vals)]
		}
		arr := array.NewPrimitivesUnsafe(values)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := Compress(arr, Options{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkRunEndDecompress(b *testing.B) {
	for _, n := range []int{1_000, 10_000, 100_000, 1_000_000} {
		codec := makeRunEndInt64(n)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := Decompress(codec); err != nil {
					b.Fatal(err)
				}
			}
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

		codec, err := Compress(array.NewPrimitivesUnsafe(values), Options{})
		if err != nil {
			return
		}

		var buf bytes.Buffer
		_, err = codec.WriteTo(&buf)
		require.NoError(t, err)

		decoded, err := Read[uint8](&buf)
		require.NoError(t, err)

		got, err := Decompress(decoded)
		require.NoError(t, err)
		require.Equal(t, values, got)
	})
}
