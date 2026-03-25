package btrblocks

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDictRoundTrip(t *testing.T) {
	t.Run("int32", func(t *testing.T) {
		pattern := []int32{10, 20, 30}
		values := make([]int32, 100)
		for i := range values {
			values[i] = pattern[i%len(pattern)]
		}

		codec, err := buildIntegerDictFromDistinct(buildArray(values), nil, newPlanContext(Options{MaxDepth: 3}))
		require.NoError(t, err)
		require.Equal(t, CodecTypeDict, codec.Encoding())
		assertRoundTrip(t, codec, values)
	})

	t.Run("float64", func(t *testing.T) {
		pattern := []float64{1.1, 2.2, 3.3, 4.4, 5.5}
		values := make([]float64, 200)
		for i := range values {
			values[i] = pattern[i%len(pattern)]
		}

		codec, err := buildFloatDictFromDistinct(buildArray(values), nil, newPlanContext(Options{MaxDepth: 3}))
		require.NoError(t, err)
		require.Equal(t, CodecTypeDict, codec.Encoding())
		assertRoundTrip(t, codec, values)
	})

	t.Run("string", func(t *testing.T) {
		pattern := []string{"alpha", "beta", "gamma", "delta"}
		values := make([]string, 100)
		for i := range values {
			values[i] = pattern[i%len(pattern)]
		}

		codec, err := buildStringDictArray(buildArray(values), newPlanContext(Options{MaxDepth: 3}))
		require.NoError(t, err)
		require.Equal(t, CodecTypeDict, codec.Encoding())
		assertRoundTrip(t, codec, values)
	})
}

func TestDictValueAt(t *testing.T) {
	values := []int32{10, 20, 30, 10, 20, 30}
	codec, err := buildIntegerDictFromDistinct(buildArray(values), nil, newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	for i, want := range values {
		require.Equal(t, want, codec.ValueAt(uint64(i)))
	}
}

func TestDictEncodingType(t *testing.T) {
	codec, err := buildIntegerDictFromDistinct(buildArray([]uint32{1, 2, 1, 2, 1, 2}), nil, newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)
	require.Equal(t, CodecTypeDict, codec.Encoding())
}

func TestDictSlicePreservesValues(t *testing.T) {
	values := []int32{10, 20, 30, 10, 20, 30, 10, 20}
	codec, err := buildIntegerDictFromDistinct(buildArray(values), nil, newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)
	require.Equal(t, CodecTypeDict, codec.Encoding())
	assertSliceRoundTrip(t, codec, 2, 6, values)
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
			for i := 0; i < b.N; i++ {
				if _, err := Compress(arr, Options{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkDictDecompress(b *testing.B) {
	for _, n := range benchSizes {
		values := makeDictBenchData(n)
		codec, err := Compress(buildArray(values), Options{})
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
		n := len(data) / 2
		if n > 512 {
			n = 512
		}
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

		codec, err := buildIntegerDictFromDistinct(buildArray(values), nil, newPlanContext(Options{MaxDepth: 3}))
		if err != nil {
			return
		}

		assertRoundTrip(t, codec, values)
	})
}
