package btrblocks

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

func TestALPRoundTripFloat64(t *testing.T) {
	pattern := []float64{12.34, 56.78}
	values := make([]float64, 256)
	for i := range values {
		values[i] = pattern[i%len(pattern)]
	}

	codec, err := buildALPArray(array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readBack, err := Read[float64](&buf)
	require.NoError(t, err)

	decoded, err := Decompress(readBack)
	require.NoError(t, err)
	require.True(t, equalFloats(values, decoded))
}

func TestALPRoundTripFloat32(t *testing.T) {
	pattern := []float32{1.5, 2.5, 3.5}
	values := make([]float32, 256)
	for i := range values {
		values[i] = pattern[i%len(pattern)]
	}

	codec, err := buildALPArray(array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readBack, err := Read[float32](&buf)
	require.NoError(t, err)

	decoded, err := Decompress(readBack)
	require.NoError(t, err)
	require.True(t, equalFloats(values, decoded))
}

func TestALPRoundTripFloat64WithPatches(t *testing.T) {
	values := make([]float64, 256)
	for i := range values {
		values[i] = 12.34
	}
	values[10] = math.Pi
	values[50] = math.Pi
	values[200] = math.Pi

	codec, err := buildALPArray(array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readBack, err := Read[float64](&buf)
	require.NoError(t, err)

	decoded, err := Decompress(readBack)
	require.NoError(t, err)
	require.True(t, equalFloats(values, decoded))
}

func TestALPEncodingType(t *testing.T) {
	pattern := []float64{12.34, 56.78}
	values := make([]float64, 256)
	for i := range values {
		values[i] = pattern[i%len(pattern)]
	}

	codec, err := buildALPArray(array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)
	require.Equal(t, CodecTypeALP, codec.Encoding())
}

func TestALPValueAtBitExact(t *testing.T) {
	pattern := []float64{12.34, 56.78}
	values := make([]float64, 256)
	for i := range values {
		values[i] = pattern[i%len(pattern)]
	}

	codec, err := buildALPArray(array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	require.Equal(t, math.Float64bits(12.34), math.Float64bits(codec.ValueAt(0)))
	require.Equal(t, math.Float64bits(56.78), math.Float64bits(codec.ValueAt(1)))
	require.Equal(t, math.Float64bits(12.34), math.Float64bits(codec.ValueAt(100)))
	require.Equal(t, math.Float64bits(56.78), math.Float64bits(codec.ValueAt(255)))
}

func TestALPEncodeDecode64Roundtrip(t *testing.T) {
	tests := []struct {
		value float64
		e, f  uint8
	}{
		{12.34, 2, 0},
		{56.78, 2, 0},
		{100.0, 1, 0},
		{0.5, 1, 0},
	}
	for _, tt := range tests {
		encoded := alpEncode64(tt.value, tt.e, tt.f)
		decoded := alpDecode64(encoded, tt.e, tt.f)
		if !alpIsException64(tt.value, tt.e, tt.f) {
			require.Equal(t, math.Float64bits(tt.value), math.Float64bits(decoded),
				"value=%v e=%d f=%d", tt.value, tt.e, tt.f)
		}
	}
}

func TestFindBestExponents64(t *testing.T) {
	values := make([]float64, 64)
	for i := range values {
		values[i] = float64(i) * 0.01
	}
	arr := array.NewPrimitivesUnsafe(values)
	e, f := findBestExponents(arr, 23, alpIsException64)
	require.True(t, e > f, "expected e > f, got e=%d f=%d", e, f)
	require.Less(t, e, uint8(23))
}

func makeALPBenchData(n int) []float64 {
	values := make([]float64, n)
	for i := range values {
		values[i] = float64(i%100)*0.01 + 100.0
	}
	return values
}

func BenchmarkALPCompress(b *testing.B) {
	for _, n := range []int{1_000, 10_000, 100_000, 1_000_000} {
		values := makeALPBenchData(n)
		arr := buildArray(values)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := Compress(arr, Options{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkALPDecompress(b *testing.B) {
	for _, n := range []int{1_000, 10_000, 100_000, 1_000_000} {
		values := makeALPBenchData(n)
		codec, err := Compress(buildArray(values), Options{})
		if err != nil {
			b.Fatal(err)
		}
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := Decompress(codec); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func FuzzALPRoundTrip(f *testing.F) {
	f.Add([]byte{0, 0, 0, 0, 0, 0, 0x28, 0x40, 0, 0, 0, 0, 0, 0, 0x59, 0x40})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 16 || len(data)%8 != 0 {
			return
		}
		values := make([]float64, 0, len(data)/8)
		for i := 0; i+8 <= len(data); i += 8 {
			v := math.Float64frombits(binary.LittleEndian.Uint64(data[i*1:]))
			if math.IsNaN(v) || math.IsInf(v, 0) {
				continue
			}
			values = append(values, v)
		}
		if len(values) < 2 {
			return
		}

		codec, err := Compress(buildArray(values), Options{})
		if err != nil {
			return
		}
		decoded, err := Decompress(codec)
		require.NoError(t, err)
		require.Equal(t, len(values), len(decoded))
		for i := range values {
			require.Equal(t, math.Float64bits(values[i]), math.Float64bits(decoded[i]),
				"mismatch at index %d", i)
		}
	})
}
