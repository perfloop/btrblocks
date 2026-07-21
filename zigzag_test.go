package btrblocks

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

func buildZigZagTest[T SignedInteger](arr array.ArrayCore[T]) (EncodedArray[T], error) {
	switch zigZagEncodedType(arr) {
	case PTypeUint8:
		return encodeZigZagAs[T, uint8](arr, testRawChild[uint8])
	case PTypeUint16:
		return encodeZigZagAs[T, uint16](arr, testRawChild[uint16])
	case PTypeUint32:
		return encodeZigZagAs[T, uint32](arr, testRawChild[uint32])
	default:
		return encodeZigZagAs[T, uint64](arr, testRawChild[uint64])
	}
}

func TestZigZagRoundTrip(t *testing.T) {
	t.Run("int32_negatives", func(t *testing.T) {
		values := make([]int32, 256)
		for i := range values {
			if i%2 == 0 {
				values[i] = -7
			} else {
				values[i] = -3
			}
		}

		codec, err := buildZigZagTest(buildArray(values))
		require.NoError(t, err)
		assertRoundTrip(t, codec, values)
	})

	t.Run("int64_mixed", func(t *testing.T) {
		values := make([]int64, 256)
		for i := range values {
			if i%2 == 0 {
				values[i] = -100
			} else {
				values[i] = 50
			}
		}

		codec, err := buildZigZagTest(buildArray(values))
		require.NoError(t, err)
		assertRoundTrip(t, codec, values)
	})

	t.Run("int8_small_negatives", func(t *testing.T) {
		values := make([]int8, 256)
		for i := range values {
			if i%2 == 0 {
				values[i] = -1
			} else {
				values[i] = -2
			}
		}

		codec, err := buildZigZagTest(buildArray(values))
		require.NoError(t, err)
		assertRoundTrip(t, codec, values)
	})
}

func TestZigZagEncodingType(t *testing.T) {
	values := make([]int32, 256)
	for i := range values {
		if i%2 == 0 {
			values[i] = -7
		} else {
			values[i] = -3
		}
	}

	codec, err := buildZigZagTest(buildArray(values))
	require.NoError(t, err)
	require.Equal(t, CodecTypeZigZag, codec.CodecType())
}

func TestZigZagValueAtNegatives(t *testing.T) {
	values := make([]int32, 256)
	for i := range values {
		if i%2 == 0 {
			values[i] = -7
		} else {
			values[i] = -3
		}
	}

	codec, err := buildZigZagTest(buildArray(values))
	require.NoError(t, err)

	require.Equal(t, int32(-7), codec.ValueAt(0))
	require.Equal(t, int32(-3), codec.ValueAt(1))
	require.Equal(t, int32(-7), codec.ValueAt(100))
	require.Equal(t, int32(-3), codec.ValueAt(255))
}

func TestZigZagEncodeValues(t *testing.T) {
	require.Equal(t, uint64(1), zigzagEncode64(-1))
	require.Equal(t, uint64(2), zigzagEncode64(1))
	require.Equal(t, uint64(0), zigzagEncode64(0))
	require.Equal(t, uint64(3), zigzagEncode64(-2))
	require.Equal(t, uint64(4), zigzagEncode64(2))
}

func makeZigZagInt32(n int) EncodedArray[int32] {
	values := make([]int32, n)
	for i := range values {
		values[i] = int32((i % 7) - 3)
	}
	codec, err := buildZigZagTest(buildArray(values))
	if err != nil {
		panic(err)
	}
	return codec
}

func BenchmarkZigZag(b *testing.B) {
	for _, n := range benchSizes {
		values := make([]int32, n)
		for i := range values {
			values[i] = int32((i % 7) - 3)
		}
		arr := buildArray(values)
		codec := makeZigZagInt32(n)

		b.Run(fmt.Sprintf("n=%d/Compress", n), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				if _, err := buildZigZagTest(arr); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("n=%d/Decompress", n), func(b *testing.B) {
			benchDecompress(b, codec)
		})
		b.Run(fmt.Sprintf("n=%d/DecompressInto", n), func(b *testing.B) {
			benchDecompressInto(b, codec)
		})
	}
}

func FuzzZigZagRoundTrip(f *testing.F) {
	f.Add([]byte{0, 0, 0, 0, 1, 0, 0, 0})
	f.Add([]byte{0xff, 0xff, 0xff, 0xff, 0x00, 0x00, 0x00, 0x00})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 4 || len(data)%4 != 0 {
			return
		}
		n := len(data) / 4
		values := make([]int32, n)
		for i := range values {
			values[i] = int32(binary.LittleEndian.Uint32(data[i*4 : (i+1)*4]))
		}

		codec, err := buildZigZagTest(buildArray(values))
		require.NoError(t, err)

		assertRoundTrip(t, codec, values)
	})
}
