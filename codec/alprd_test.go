package codec

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

func buildALPRD32Test(arr array.ArrayCore[float32]) (EncodedArray[float32], error) {
	return encodeALPRD32(arr, testUnsignedChildren{}, testBuildBudget)
}

func buildALPRD64Test(arr array.ArrayCore[float64]) (EncodedArray[float64], error) {
	return encodeALPRD64(arr, testUnsignedChildren{}, testBuildBudget)
}

func TestALPRDRoundTrip(t *testing.T) {
	t.Run("float64", func(t *testing.T) {
		values := make([]float64, 256)
		for i := range values {
			values[i] = 1.0 + float64(i)*1e-10
		}
		codec, err := buildALPRD64Test(array.NewPrimitivesUnsafe(values))
		require.NoError(t, err)
		require.Equal(t, CodecTypeALPRD, codec.CodecType())
		assertRoundTrip(t, codec, values)
	})
	t.Run("float32", func(t *testing.T) {
		values := make([]float32, 256)
		for i := range values {
			values[i] = float32(1.0) + float32(i)*1e-5
		}
		codec, err := buildALPRD32Test(array.NewPrimitivesUnsafe(values))
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
		codec, err := buildALPRD64Test(array.NewPrimitivesUnsafe(values))
		require.NoError(t, err)
		assertRoundTrip(t, codec, values)
	})
}

func TestALPRDDictSizeSmall(t *testing.T) {
	values := make([]float64, 256)
	for i := range values {
		values[i] = 1.0 + float64(i)*1e-10
	}
	codec, err := buildALPRD64Test(array.NewPrimitivesUnsafe(values))
	require.NoError(t, err)

	alprd, ok := codec.(*alprdArray[float64, uint64])
	if !ok {
		alprd2, ok2 := codec.(*alprdArray[float64, uint8])
		require.True(t, ok2)
		require.True(t, alprd2.dictSize <= uint8(8))
		return
	}
	require.True(t, alprd.dictSize <= uint8(8))
}

func TestALPRDEncodingDeterministicWithTiedFrequencies(t *testing.T) {
	values := make([]float64, 0, 16*64)
	for repeat := range 64 {
		for prefix := range 16 {
			bits := uint64(prefix)<<48 | uint64(repeat)
			values = append(values, math.Float64frombits(bits))
		}
	}

	var first []byte
	for range 32 {
		encoded, err := buildALPRD64Test(buildArray(values))
		require.NoError(t, err)
		data := mustWriteEncodedArray(t, encoded)
		if first == nil {
			first = data
			continue
		}
		require.Equal(t, first, data)
	}
}

func TestALPRDHighPatchRatioError(t *testing.T) {
	require.True(t, errALPRDHighPatchRatio != nil)
	require.ErrorContains(t, errALPRDHighPatchRatio, "patch ratio")
}

// TestALPRDCompressLargeArrayDoesNotPanic verifies ALPRD estimation works
// on arrays large enough to trigger sampling.
func TestALPRDCompressLargeArrayDoesNotPanic(t *testing.T) {
	values := make([]float64, 10_000)
	for i := range values {
		values[i] = 1.0 / float64(i+1)
	}
	codec, err := buildALPRD64Test(buildArray(values))
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
	codec, err := buildALPRD64Test(array.NewPrimitivesUnsafe(values))
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
			for b.Loop() {
				if _, err := buildALPRD64Test(arr); err != nil {
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
		for i := range count {
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

		codec, err := buildALPRD64Test(buildArray(values))
		if err != nil {
			return
		}

		decoded, err := Decompress(codec)
		require.NoError(t, err)
		assertValuesEqual(t, values, decoded)
	})
}

func makeALPRDWithPatches(n int) EncodedArray[float64] {
	values := make([]float64, n)
	for i := range values {
		values[i] = 1.0 + float64(i)*1e-10
	}
	for i := 0; i < n; i += 50 {
		values[i] = 1e100
	}
	codec, err := buildALPRD64Test(array.NewPrimitivesUnsafe(values))
	if err != nil {
		codec2, err2 := buildALPRD64Test(buildArray(values))
		if err2 != nil {
			panic(err2)
		}
		return codec2
	}
	return codec
}

func BenchmarkALPRDDecompressIntoPatched(b *testing.B) {
	for _, n := range []int{10_000, 100_000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			benchDecompressInto(b, makeALPRDWithPatches(n))
		})
	}
}

// TestReadALPRDRejectsInvalidBitWidths guards the fixed-size left dictionary
// and the bit unpackers against crafted width bytes.
func TestReadALPRDRejectsInvalidBitWidths(t *testing.T) {
	encode := func(rightBW, leftBW byte) []byte {
		body := make([]byte, 0, 27)
		body = append(body, rightBW, leftBW, alprdMaxDictSize)
		body = append(body, make([]byte, alprdMaxDictSize*2)...) // dict entries
		body = binary.LittleEndian.AppendUint32(body, 0)         // left buffer length
		body = binary.LittleEndian.AppendUint32(body, 0)         // right buffer length
		var buf bytes.Buffer
		h := codecHeader{Version: versionNumber, Type: CodecTypeALPRD, ElemType: PTypeFloat64, Length: 1, NumBytes: uint64(len(body))}
		if _, err := h.WriteTo(&buf); err != nil {
			panic(err)
		}
		buf.Write(body)
		return buf.Bytes()
	}

	_, err := LoadFloat64(encode(32, 8))
	require.ErrorContains(t, err, "left bit width")

	_, err = LoadFloat64(encode(200, 3))
	require.ErrorContains(t, err, "right bit width")
}
