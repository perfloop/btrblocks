package btrblocks

import (
	"fmt"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

func buildDeltaTest[T Integer](arr array.ArrayCore[T]) (EncodedArray[T], error) {
	return encodeDelta(arr, testRawChild[T])
}

func TestDeltaRoundTrip(t *testing.T) {
	t.Run("uint32_monotonic", func(t *testing.T) {
		values := []uint32{1000, 1005, 1010, 1015, 1020}
		codec, err := buildDeltaTest(array.NewPrimitivesUnsafe(values))
		require.NoError(t, err)
		assertRoundTrip(t, codec, values)
	})

	t.Run("uint64_timestamps", func(t *testing.T) {
		values := make([]uint64, 200)
		for i := range values {
			values[i] = 1_000_000 + uint64(i)*10
		}
		codec, err := buildDeltaTest(array.NewPrimitivesUnsafe(values))
		require.NoError(t, err)
		assertRoundTrip(t, codec, values)
	})

	t.Run("int32_slowly_varying", func(t *testing.T) {
		values := []int32{100, 102, 101, 103, 105, 104, 106}
		codec, err := buildDeltaTest(array.NewPrimitivesUnsafe(values))
		require.NoError(t, err)
		assertRoundTrip(t, codec, values)
	})

	t.Run("int64_negative_deltas", func(t *testing.T) {
		values := []int64{1000, 900, 800, 700, 600}
		codec, err := buildDeltaTest(array.NewPrimitivesUnsafe(values))
		require.NoError(t, err)
		assertRoundTrip(t, codec, values)
	})
}

func TestDeltaEncodingIsCodecTypeDelta(t *testing.T) {
	values := []uint32{1000, 1005, 1010, 1015, 1020}
	codec, err := buildDeltaTest(array.NewPrimitivesUnsafe(values))
	require.NoError(t, err)
	require.Equal(t, CodecTypeDelta, codec.CodecType())
}

func TestDeltaSlice(t *testing.T) {
	values := []uint32{1000, 1005, 1010, 1015, 1020}
	codec, err := buildDeltaTest(array.NewPrimitivesUnsafe(values))
	require.NoError(t, err)
	assertSliceRoundTrip(t, codec, 1, 4, values)
}

func TestDeltaRejectsShortArrays(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		_, err := buildDeltaTest(array.NewPrimitivesUnsafe([]uint32{}))
		require.ErrorIs(t, err, errDataEmpty)
	})
	t.Run("single", func(t *testing.T) {
		_, err := buildDeltaTest(array.NewPrimitivesUnsafe([]uint32{42}))
		require.ErrorIs(t, err, errDataEmpty)
	})
}

func BenchmarkDeltaUint64(b *testing.B) {
	for _, n := range benchSizes {
		values := make([]uint64, n)
		for i := range values {
			values[i] = 1_000_000 + uint64(i)*10
		}

		b.Run(fmt.Sprintf("compress/%d", n), func(b *testing.B) {
			arr := buildArray(values)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := buildDeltaTest(arr); err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run(fmt.Sprintf("decompress/%d", n), func(b *testing.B) {
			arr := buildArray(values)
			codec, err := buildDeltaTest(arr)
			if err != nil {
				b.Fatal(err)
			}
			benchDecompress(b, codec)
		})

		b.Run(fmt.Sprintf("decompressInto/%d", n), func(b *testing.B) {
			arr := buildArray(values)
			codec, err := buildDeltaTest(arr)
			if err != nil {
				b.Fatal(err)
			}
			benchDecompressInto(b, codec)
		})
	}
}

func FuzzDeltaUint32Roundtrip(f *testing.F) {
	f.Add(uint32(1000), []byte{0, 5, 10, 15, 20})
	f.Add(uint32(0), []byte{1, 1, 1, 1})

	f.Fuzz(func(t *testing.T, base uint32, offsets []byte) {
		if len(offsets) < 2 {
			return
		}

		values := make([]uint32, len(offsets))
		values[0] = base
		for i := 1; i < len(offsets); i++ {
			values[i] = values[i-1] + uint32(offsets[i])
		}

		codec, err := buildDeltaTest(array.NewPrimitivesUnsafe(values))
		if err != nil {
			return
		}

		assertRoundTrip(t, codec, values)
	})
}
