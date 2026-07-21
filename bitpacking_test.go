package btrblocks

import (
	"fmt"
	"testing"
	"unsafe"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

func TestBitpackRoundTrip(t *testing.T) {
	t.Run("uint32_small_values", func(t *testing.T) {
		values := make([]uint32, 256)
		for i := range values {
			values[i] = uint32(i % 16)
		}
		codec, err := encodeBitpack(array.NewPrimitivesUnsafe(values))
		require.NoError(t, err)
		assertRoundTrip(t, codec, values)
	})

	t.Run("uint8_two_bit_values", func(t *testing.T) {
		values := make([]uint8, 256)
		for i := range values {
			values[i] = uint8(i % 4)
		}
		codec, err := encodeBitpack(array.NewPrimitivesUnsafe(values))
		require.NoError(t, err)
		assertRoundTrip(t, codec, values)
	})

	t.Run("with_patches", func(t *testing.T) {
		values := make([]uint32, 1024)
		for i := range values {
			values[i] = uint32(i % 8)
		}
		values[100] = 1 << 20
		values[500] = 1<<20 + 7
		codec, err := encodeBitpack(array.NewPrimitivesUnsafe(values))
		require.NoError(t, err)
		assertRoundTrip(t, codec, values)
	})
}

func TestBitpackPatchesPresent(t *testing.T) {
	values := make([]uint32, 1024)
	for i := range values {
		values[i] = uint32(i % 8)
	}
	values[100] = 1 << 20
	values[500] = 1<<20 + 7

	codec, err := encodeBitpack(array.NewPrimitivesUnsafe(values))
	require.NoError(t, err)

	bitpack, ok := codec.(*bitPackedArray[uint32, uint64])
	require.True(t, ok)
	require.True(t, bitpack.patches != nil)
	require.True(t, bitpack.bitWidth < bitWidthForUnsigned(uint64(values[100])))
}

func TestBitpackValueAtIncludingPatches(t *testing.T) {
	values := make([]uint32, 1024)
	for i := range values {
		values[i] = uint32(i % 8)
	}
	values[100] = 1 << 20
	values[500] = 1<<20 + 7

	codec, err := encodeBitpack(array.NewPrimitivesUnsafe(values))
	require.NoError(t, err)

	bitpack, ok := codec.(*bitPackedArray[uint32, uint64])
	require.True(t, ok)

	require.Equal(t, values[0], bitpack.ValueAt(0))
	require.Equal(t, values[7], bitpack.ValueAt(7))
	require.Equal(t, values[100], bitpack.ValueAt(100))
	require.Equal(t, values[500], bitpack.ValueAt(500))
	require.Equal(t, values[1023], bitpack.ValueAt(1023))
}

func TestBitpackZeroWidthAllZeros(t *testing.T) {
	values := make([]uint32, 256)

	codec, err := encodeBitpack(array.NewPrimitivesUnsafe(values))
	require.NoError(t, err)

	bitpack, ok := codec.(*bitPackedArray[uint32, uint64])
	require.True(t, ok)
	require.Equal(t, uint(0), bitpack.bitWidth)

	assertRoundTrip(t, codec, values)
}

func makeBitpackUint32(n int) (EncodedArray[uint32], []uint32) {
	values := make([]uint32, n)
	for i := range values {
		values[i] = uint32(i % 16)
	}
	codec, err := encodeBitpack(array.NewPrimitivesUnsafe(values))
	if err != nil {
		panic(err)
	}
	return codec, values
}

func BenchmarkBitpackCompress(b *testing.B) {
	for _, n := range benchSizes {
		values := make([]uint32, n)
		for i := range values {
			values[i] = uint32(i % 16)
		}
		arr := array.NewPrimitivesUnsafe(values)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				if _, err := encodeBitpack(arr); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkBitpackDecompress(b *testing.B) {
	for _, n := range benchSizes {
		codec, _ := makeBitpackUint32(n)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			benchDecompress(b, codec)
		})
	}
}

func BenchmarkBitpackPatchedDecompressInto(b *testing.B) {
	for _, n := range benchSizes[:3] {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			benchDecompressInto(b, makeBitpackWithPatches(n))
		})
	}
}

func FuzzBitpackRoundTrip(f *testing.F) {
	f.Add([]byte{0, 0, 0, 0, 1, 0, 0, 0, 2, 0, 0, 0, 3, 0, 0, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 4 || len(data)%4 != 0 {
			return
		}
		n := len(data) / 4
		values := unsafe.Slice((*uint32)(unsafe.Pointer(&data[0])), n)

		input := make([]uint32, n)
		copy(input, values)

		codec, err := encodeBitpack(array.NewPrimitivesUnsafe(input))
		if err != nil {
			return
		}

		decoded, err := Decompress(codec)
		require.NoError(t, err)
		require.Equal(t, input, decoded)
	})
}

// makeBitpackWithPatches creates a bitpacked array where ~5% of values exceed
// the chosen bit width and become patches.
func makeBitpackWithPatches(n int) EncodedArray[uint32] {
	values := make([]uint32, n)
	for i := range values {
		if i%20 == 0 {
			values[i] = 100_000 // exceeds small bit width
		} else {
			values[i] = uint32(i % 16) // fits in 4 bits
		}
	}
	// Build the selected codec directly; Compress chooses a planner scheme and
	// is not a way to force a particular codec for a fixture.
	codec, err := encodeBitpack(array.NewPrimitivesUnsafe(values))
	if err != nil {
		panic(err)
	}
	if codec.CodecType() != CodecTypeBitpack {
		panic("expected bitpack encoding, got " + codec.CodecType().String())
	}
	return codec
}
