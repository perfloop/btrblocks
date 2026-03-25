package btrblocks

import (
	"encoding/binary"
	"fmt"
	"math"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

func TestALPRDRoundTrip(t *testing.T) {
	t.Run("float64", func(t *testing.T) {
		values := make([]float64, 256)
		for i := range values {
			values[i] = 1.0 + float64(i)*1e-10
		}
		codec, err := buildALPRDArray[float64](array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}))
		require.NoError(t, err)
		require.Equal(t, CodecTypeALPRD, codec.Encoding())
		assertRoundTrip(t, codec, values)
	})
	t.Run("float32", func(t *testing.T) {
		values := make([]float32, 256)
		for i := range values {
			values[i] = float32(1.0) + float32(i)*1e-5
		}
		codec, err := buildALPRDArray[float32](array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}))
		require.NoError(t, err)
		assertRoundTrip(t, codec, values)
	})
	t.Run("float64 with patches", func(t *testing.T) {
		values := make([]float64, 256)
		for i := range values {
			values[i] = 1.0 + float64(i)*1e-10
		}
		values[50] = 1e100
		values[100] = -3.14
		values[200] = 1e-300
		codec, err := buildALPRDArray[float64](array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}))
		require.NoError(t, err)
		assertRoundTrip(t, codec, values)
	})
}

func TestALPRDDictSizeSmall(t *testing.T) {
	values := make([]float64, 256)
	for i := range values {
		values[i] = 1.0 + float64(i)*1e-10
	}
	codec, err := buildALPRDArray[float64](array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	alprd, ok := codec.(*alprdArray[float64, uint64])
	if !ok {
		alprd2, ok2 := codec.(*alprdArray[float64, uint8])
		require.True(t, ok2)
		require.LessOrEqual(t, alprd2.dictSize, uint8(8))
		return
	}
	require.LessOrEqual(t, alprd.dictSize, uint8(8))
}

func TestALPRDHighPatchRatioError(t *testing.T) {
	require.NotNil(t, errALPRDHighPatchRatio)
	require.Contains(t, errALPRDHighPatchRatio.Error(), "patch ratio")
}

// TestALPRDCompressLargeArrayDoesNotPanic verifies ALPRD estimation works
// on arrays large enough to trigger sampling.
func TestALPRDCompressLargeArrayDoesNotPanic(t *testing.T) {
	values := make([]float64, 10_000)
	for i := range values {
		values[i] = 1.0 / float64(i+1)
	}
	opts := EmptySchemes().WithIncludeFloat(CodecTypeALPRD)
	codec, err := Compress(buildArray(values), opts)
	require.NoError(t, err)

	decoded, err := Decompress(codec)
	require.NoError(t, err)
	require.Equal(t, len(values), len(decoded))
}

func makeALPRDFloat64(n int) EncodedArray[float64] {
	values := make([]float64, n)
	for i := range values {
		values[i] = 1.0 + float64(i)*1e-12
	}
	codec, err := Compress(array.NewPrimitivesUnsafe(values), Options{})
	if err != nil {
		panic(err)
	}
	return codec
}

func BenchmarkALPRD(b *testing.B) {
	for _, n := range benchSizes {
		values := make([]float64, n)
		for i := range values {
			values[i] = 1.0 + float64(i)*1e-12
		}
		arr := buildArray(values)

		b.Run(fmt.Sprintf("compress/%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := Compress(arr, Options{}); err != nil {
					b.Fatal(err)
				}
			}
		})

		codec := makeALPRDFloat64(n)
		b.Run(fmt.Sprintf("decompress/%d", n), func(b *testing.B) {
			benchDecompress(b, codec)
		})
		b.Run(fmt.Sprintf("decompressInto/%d", n), func(b *testing.B) {
			benchDecompressInto(b, codec)
		})
	}
}

func FuzzALPRDRoundTrip(f *testing.F) {
	seed := make([]byte, 64)
	for i := range seed {
		seed[i] = byte(i)
	}
	f.Add(seed)

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 64 {
			return
		}

		count := len(data) / 8
		values := make([]float64, 0, count)
		for i := 0; i < count; i++ {
			bits := binary.LittleEndian.Uint64(data[i*8:])
			v := math.Float64frombits(bits)
			if math.IsNaN(v) || math.IsInf(v, 0) {
				continue
			}
			values = append(values, v)
		}

		if len(values) < 8 {
			return
		}

		codec, err := Compress(buildArray(values), Options{})
		if err != nil {
			return
		}

		decoded, err := Decompress(codec)
		require.NoError(t, err)
		assertValuesEqual(t, values, decoded)
	})
}
