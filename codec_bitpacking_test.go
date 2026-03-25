package btrblocks

import (
	"bytes"
	"fmt"
	"testing"
	"unsafe"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

func TestBitpackRoundTripUint32SmallValues(t *testing.T) {
	values := make([]uint32, 256)
	for i := range values {
		values[i] = uint32(i % 16)
	}

	codec, err := buildBitPackedArray(array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readBack, err := Load[uint32](buf.Bytes())
	require.NoError(t, err)

	decoded, err := Decompress(readBack)
	require.NoError(t, err)
	require.Equal(t, values, decoded)
}

func TestBitpackRoundTripUint8TwoBitValues(t *testing.T) {
	values := make([]uint8, 256)
	for i := range values {
		values[i] = uint8(i % 4)
	}

	codec, err := buildBitPackedArray(array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readBack, err := Load[uint8](buf.Bytes())
	require.NoError(t, err)

	decoded, err := Decompress(readBack)
	require.NoError(t, err)
	require.Equal(t, values, decoded)
}

func TestBitpackRoundTripWithPatches(t *testing.T) {
	values := make([]uint32, 1024)
	for i := range values {
		values[i] = uint32(i % 8)
	}
	values[100] = 1 << 20
	values[500] = 1<<20 + 7

	codec, err := buildBitPackedArray(array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readBack, err := Load[uint32](buf.Bytes())
	require.NoError(t, err)

	decoded, err := Decompress(readBack)
	require.NoError(t, err)
	require.Equal(t, values, decoded)
}

func TestBitpackPatchesPresent(t *testing.T) {
	values := make([]uint32, 1024)
	for i := range values {
		values[i] = uint32(i % 8)
	}
	values[100] = 1 << 20
	values[500] = 1<<20 + 7

	codec, err := buildBitPackedArray(array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	bitpack, ok := codec.(*bitPackedArray[uint32, uint64])
	require.True(t, ok)
	require.NotNil(t, bitpack.patches)
	require.Less(t, bitpack.bitWidth, bitWidthForUnsigned(uint64(values[100])))
}

func TestBitpackValueAtIncludingPatches(t *testing.T) {
	values := make([]uint32, 1024)
	for i := range values {
		values[i] = uint32(i % 8)
	}
	values[100] = 1 << 20
	values[500] = 1<<20 + 7

	codec, err := buildBitPackedArray(array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}))
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

	codec, err := buildBitPackedArray(array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	bitpack, ok := codec.(*bitPackedArray[uint32, uint64])
	require.True(t, ok)
	require.Equal(t, uint(0), bitpack.bitWidth)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readBack, err := Load[uint32](buf.Bytes())
	require.NoError(t, err)

	decoded, err := Decompress(readBack)
	require.NoError(t, err)
	require.Equal(t, values, decoded)
}

func makeBitpackUint32(n int) (EncodedArray[uint32], []uint32) {
	values := make([]uint32, n)
	for i := range values {
		values[i] = uint32(i % 16)
	}
	codec, err := buildBitPackedArray(array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}))
	if err != nil {
		panic(err)
	}
	return codec, values
}

func BenchmarkBitpackCompress(b *testing.B) {
	for _, n := range []int{1_000, 10_000, 100_000, 1_000_000} {
		values := make([]uint32, n)
		for i := range values {
			values[i] = uint32(i % 16)
		}
		arr := array.NewPrimitivesUnsafe(values)
		ctx := newPlanContext(Options{MaxDepth: 3})
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := buildBitPackedArray(arr, ctx); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkBitpackDecompress(b *testing.B) {
	for _, n := range []int{1_000, 10_000, 100_000, 1_000_000} {
		codec, _ := makeBitpackUint32(n)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := Decompress(codec); err != nil {
					b.Fatal(err)
				}
			}
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

		codec, err := buildBitPackedArray(array.NewPrimitivesUnsafe(input), newPlanContext(Options{MaxDepth: 3}))
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
	// Force bitpack only
	opts := EmptySchemes().WithIncludeInteger(CodecTypeBitpack)
	codec, err := Compress(array.NewPrimitivesUnsafe(values), opts)
	if err != nil {
		panic(err)
	}
	if codec.Encoding() != CodecTypeBitpack {
		panic("expected bitpack encoding, got " + codec.Encoding().String())
	}
	return codec
}

func BenchmarkBitpackPatchedDecompressInto_1K(b *testing.B) {
	benchDecompressInto(b, makeBitpackWithPatches(1_000))
}

func BenchmarkBitpackPatchedDecompressInto_10K(b *testing.B) {
	benchDecompressInto(b, makeBitpackWithPatches(10_000))
}

func BenchmarkBitpackPatchedDecompressInto_100K(b *testing.B) {
	benchDecompressInto(b, makeBitpackWithPatches(100_000))
}
