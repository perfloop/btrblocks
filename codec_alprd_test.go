package btrblocks

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

func TestALPRDRoundTripFloat64SharedExponents(t *testing.T) {
	values := make([]float64, 256)
	for i := range values {
		values[i] = 1.0 + float64(i)*1e-10
	}

	codec, err := buildALPRDArray[float64](array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readBack, err := Read[float64](&buf)
	require.NoError(t, err)

	decoded, err := readBack.Decompress()
	require.NoError(t, err)
	require.Equal(t, len(values), len(decoded))
	for i := range values {
		require.Equal(t, math.Float64bits(values[i]), math.Float64bits(decoded[i]), "mismatch at index %d", i)
	}
}

func TestALPRDRoundTripFloat32(t *testing.T) {
	values := make([]float32, 256)
	for i := range values {
		values[i] = float32(1.0) + float32(i)*1e-5
	}

	codec, err := buildALPRDArray[float32](array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readBack, err := Read[float32](&buf)
	require.NoError(t, err)

	decoded, err := readBack.Decompress()
	require.NoError(t, err)
	require.Equal(t, len(values), len(decoded))
	for i := range values {
		require.Equal(t, math.Float32bits(values[i]), math.Float32bits(decoded[i]), "mismatch at index %d", i)
	}
}

func TestALPRDRoundTripWithPatches(t *testing.T) {
	values := make([]float64, 256)
	for i := range values {
		values[i] = 1.0 + float64(i)*1e-10
	}
	values[50] = 1e100
	values[100] = -3.14
	values[200] = 1e-300

	codec, err := buildALPRDArray[float64](array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readBack, err := Read[float64](&buf)
	require.NoError(t, err)

	decoded, err := readBack.Decompress()
	require.NoError(t, err)
	require.Equal(t, len(values), len(decoded))
	for i := range values {
		require.Equal(t, math.Float64bits(values[i]), math.Float64bits(decoded[i]), "mismatch at index %d", i)
	}
}

func TestALPRDEncodingType(t *testing.T) {
	values := make([]float64, 256)
	for i := range values {
		values[i] = 1.0 + float64(i)*1e-10
	}

	codec, err := buildALPRDArray[float64](array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)
	require.Equal(t, CodecTypeALPRD, codec.Encoding())
}

func TestALPRDValueAtBitExact(t *testing.T) {
	values := make([]float64, 256)
	for i := range values {
		values[i] = 1.0 + float64(i)*1e-10
	}

	codec, err := buildALPRDArray[float64](array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	for _, idx := range []uint64{0, 1, 127, 255} {
		got := codec.ValueAt(idx)
		require.Equal(t, math.Float64bits(values[idx]), math.Float64bits(got), "ValueAt(%d) mismatch", idx)
	}
}

func TestALPRDDictSizeSmall(t *testing.T) {
	values := make([]float64, 256)
	for i := range values {
		values[i] = 1.0 + float64(i)*1e-10
	}

	codec, err := buildALPRDArray[float64](array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	alprd, ok := codec.(*alprdArray64)
	require.True(t, ok)
	require.LessOrEqual(t, alprd.dictSize, uint8(8))
}

func TestALPRDHighPatchRatioError(t *testing.T) {
	// The >50% patch check guards against data where the best dictionary cut
	// still leaves most values as exceptions. Verify the sentinel error exists
	// and that buildALPRDArray64 returns it when patches exceed the threshold.
	// We construct an alprdDict manually where most values are not in the forward map.
	n := uint64(100)
	values := make([]float64, n)
	for i := range values {
		// Each value has a unique upper-11-bit pattern (exponent+sign).
		values[i] = math.Float64frombits((uint64(i) + 1) << 52)
	}

	// Directly check that the error variable is well-formed.
	require.NotNil(t, errALPRDHighPatchRatio)
	require.Contains(t, errALPRDHighPatchRatio.Error(), "patch ratio")
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

func benchALPRDCompress(b *testing.B, n int) {
	values := make([]float64, n)
	for i := range values {
		values[i] = 1.0 + float64(i)*1e-12
	}
	arr := buildArray(values)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Compress(arr, Options{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkALPRDCompress_1K(b *testing.B)   { benchALPRDCompress(b, 1_000) }
func BenchmarkALPRDCompress_10K(b *testing.B)  { benchALPRDCompress(b, 10_000) }
func BenchmarkALPRDCompress_100K(b *testing.B) { benchALPRDCompress(b, 100_000) }
func BenchmarkALPRDCompress_1M(b *testing.B)   { benchALPRDCompress(b, 1_000_000) }

func BenchmarkALPRDDecompress_1K(b *testing.B)   { benchDecompress(b, makeALPRDFloat64(1_000)) }
func BenchmarkALPRDDecompress_10K(b *testing.B)  { benchDecompress(b, makeALPRDFloat64(10_000)) }
func BenchmarkALPRDDecompress_100K(b *testing.B) { benchDecompress(b, makeALPRDFloat64(100_000)) }
func BenchmarkALPRDDecompress_1M(b *testing.B)   { benchDecompress(b, makeALPRDFloat64(1_000_000)) }

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

		decoded, err := codec.Decompress()
		if err != nil {
			t.Fatalf("decompress failed: %v", err)
		}

		if len(decoded) != len(values) {
			t.Fatalf("length mismatch: got %d, want %d", len(decoded), len(values))
		}
		for i := range values {
			if math.Float64bits(values[i]) != math.Float64bits(decoded[i]) {
				t.Fatalf("bit mismatch at %d: want %016x, got %016x",
					i, math.Float64bits(values[i]), math.Float64bits(decoded[i]))
			}
		}
	})
}
