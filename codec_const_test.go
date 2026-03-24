package btrblocks

import (
	"bytes"
	"fmt"
	"math"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

func TestConstRoundTripInt32(t *testing.T) {
	values := []int32{42, 42, 42, 42, 42}
	codec, err := newConstIntegerArray(array.NewPrimitivesUnsafe(values))
	require.NoError(t, err)
	require.Equal(t, CodecTypeConst, codec.Encoding())
	require.Equal(t, uint64(len(values)), codec.Length())

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readBack, err := Read[int32](&buf)
	require.NoError(t, err)

	decoded, err := readBack.Decompress()
	require.NoError(t, err)
	require.Equal(t, values, decoded)
}

func TestConstRoundTripFloat64(t *testing.T) {
	values := []float64{3.14, 3.14, 3.14, 3.14}
	codec, err := newConstFloatArray(array.NewPrimitivesUnsafe(values))
	require.NoError(t, err)
	require.Equal(t, CodecTypeConst, codec.Encoding())

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readBack, err := Read[float64](&buf)
	require.NoError(t, err)

	decoded, err := readBack.Decompress()
	require.NoError(t, err)
	require.True(t, equalFloats(values, decoded))
}

func TestConstRoundTripString(t *testing.T) {
	values := []string{"hello", "hello", "hello"}
	codec, err := newConstStringArray(array.NewStrings(values))
	require.NoError(t, err)
	require.Equal(t, CodecTypeConst, codec.Encoding())

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readBack, err := Read[string](&buf)
	require.NoError(t, err)

	decoded, err := readBack.Decompress()
	require.NoError(t, err)
	require.Equal(t, values, decoded)
}

func TestConstNonConstantDataReturnsError(t *testing.T) {
	_, err := newConstIntegerArray(array.NewPrimitivesUnsafe([]int32{1, 2, 1}))
	require.ErrorIs(t, err, errValueNotConstant)

	_, err = newConstFloatArray(array.NewPrimitivesUnsafe([]float64{1.0, 2.0}))
	require.ErrorIs(t, err, errValueNotConstant)

	_, err = newConstStringArray(array.NewStrings([]string{"a", "b"}))
	require.ErrorIs(t, err, errValueNotConstant)
}

func TestConstValueAtReturnsCorrectValue(t *testing.T) {
	values := []int32{7, 7, 7, 7, 7}
	codec, err := newConstIntegerArray(array.NewPrimitivesUnsafe(values))
	require.NoError(t, err)

	for i := uint64(0); i < codec.Length(); i++ {
		require.Equal(t, int32(7), codec.ValueAt(i))
	}
}

func TestConstValueAtFloat64NaN(t *testing.T) {
	nan := math.NaN()
	values := []float64{nan, nan, nan}
	codec, err := newConstFloatArray(array.NewPrimitivesUnsafe(values))
	require.NoError(t, err)

	for i := uint64(0); i < codec.Length(); i++ {
		require.True(t, math.IsNaN(codec.ValueAt(i)))
	}
}

func TestConstSlicePreservesEncoding(t *testing.T) {
	values := []uint64{99, 99, 99, 99, 99}
	codec, err := newConstIntegerArray(array.NewPrimitivesUnsafe(values))
	require.NoError(t, err)

	sliced, err := codec.Slice(1, 4)
	require.NoError(t, err)
	require.Equal(t, CodecTypeConst, sliced.Encoding())
	require.Equal(t, uint64(3), sliced.Length())

	decoded, err := sliced.Decompress()
	require.NoError(t, err)
	require.Equal(t, []uint64{99, 99, 99}, decoded)
}

func BenchmarkConstUint64(b *testing.B) {
	sizes := []int{1_000, 10_000, 100_000, 1_000_000}

	for _, n := range sizes {
		data := make([]uint64, n)
		for i := range data {
			data[i] = 42
		}

		b.Run(fmt.Sprintf("compress/%d", n), func(b *testing.B) {
			arr := array.NewPrimitivesUnsafe(data)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				codec, err := newConstIntegerArray(arr)
				if err != nil {
					b.Fatal(err)
				}
				var buf bytes.Buffer
				_, err = codec.WriteTo(&buf)
				if err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run(fmt.Sprintf("decompress/%d", n), func(b *testing.B) {
			arr := array.NewPrimitivesUnsafe(data)
			codec, err := newConstIntegerArray(arr)
			if err != nil {
				b.Fatal(err)
			}
			var buf bytes.Buffer
			_, err = codec.WriteTo(&buf)
			if err != nil {
				b.Fatal(err)
			}
			encoded := buf.Bytes()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				r := bytes.NewReader(encoded)
				readBack, err := Read[uint64](r)
				if err != nil {
					b.Fatal(err)
				}
				_, err = readBack.Decompress()
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

		allSame := true
		for i := 1; i < len(data); i++ {
			if data[i] != data[0] {
				allSame = false
				break
			}
		}
		if !allSame {
			return
		}

		codec, err := newConstIntegerArray(array.NewPrimitivesUnsafe(data))
		require.NoError(t, err)

		var buf bytes.Buffer
		_, err = codec.WriteTo(&buf)
		require.NoError(t, err)

		readBack, err := Read[uint8](&buf)
		require.NoError(t, err)

		decoded, err := readBack.Decompress()
		require.NoError(t, err)
		require.Equal(t, data, decoded)
	})
}
