package codec

import (
	"fmt"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

func TestFoRRoundTrip(t *testing.T) {
	t.Run("uint32_high_base", func(t *testing.T) {
		values := []uint32{1000, 1002, 1004, 1006, 1008}
		codec, err := encodeFoR(array.NewPrimitivesUnsafe(values), testBuildBudget)
		require.NoError(t, err)
		assertRoundTrip(t, codec, values)
	})

	t.Run("uint64_narrow_range", func(t *testing.T) {
		values := make([]uint64, 200)
		for i := range values {
			values[i] = 1_000_000 + uint64(i%100)
		}
		codec, err := encodeFoR(array.NewPrimitivesUnsafe(values), testBuildBudget)
		require.NoError(t, err)
		assertRoundTrip(t, codec, values)
	})
}

func TestFoREncodingIsCodecTypeFor(t *testing.T) {
	values := []uint32{1000, 1002, 1004, 1006, 1008}
	codec, err := encodeFoR(array.NewPrimitivesUnsafe(values), testBuildBudget)
	require.NoError(t, err)
	require.Equal(t, CodecTypeFor, codec.CodecType())
}

func TestFoRChildIsBitPacked(t *testing.T) {
	values := []uint32{1000, 1002, 1004, 1006, 1008}
	codec, err := encodeFoR(array.NewPrimitivesUnsafe(values), testBuildBudget)
	require.NoError(t, err)

	f, ok := codec.(*forArray[uint32])
	require.True(t, ok)
	if _, ok := f.child.(*bitPackedArray[uint32, uint64]); !ok {
		t.Fatalf("child = %T, want *bitPackedArray[uint32, uint64]", f.child)
	}
}

func TestFoRSlice(t *testing.T) {
	values := []uint32{1000, 1002, 1004, 1006, 1008}
	codec, err := encodeFoR(array.NewPrimitivesUnsafe(values), testBuildBudget)
	require.NoError(t, err)

	require.Equal(t, CodecTypeFor, codec.CodecType())
	assertSliceRoundTrip(t, codec, 1, 4, values)
}

func BenchmarkFoRUint32(b *testing.B) {
	for _, n := range benchSizes {
		values := make([]uint32, n)
		for i := range values {
			values[i] = 1_000_000 + uint32(i%64)
		}

		b.Run(fmt.Sprintf("compress/%d", n), func(b *testing.B) {
			arr := buildArray(values)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := encodeFoR(arr, testBuildBudget); err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run(fmt.Sprintf("decompress/%d", n), func(b *testing.B) {
			arr := buildArray(values)
			codec, err := encodeFoR(arr, testBuildBudget)
			if err != nil {
				b.Fatal(err)
			}
			benchDecompress(b, codec)
		})

		b.Run(fmt.Sprintf("decompressInto/%d", n), func(b *testing.B) {
			arr := buildArray(values)
			codec, err := encodeFoR(arr, testBuildBudget)
			if err != nil {
				b.Fatal(err)
			}
			benchDecompressInto(b, codec)
		})
	}
}

func FuzzFoRUint32Roundtrip(f *testing.F) {
	f.Add(uint32(1000), []byte{0, 2, 4, 6, 8})
	f.Add(uint32(500000), []byte{1, 1, 1, 1})

	f.Fuzz(func(t *testing.T, base uint32, offsets []byte) {
		if len(offsets) == 0 || base == 0 {
			return
		}

		values := make([]uint32, len(offsets))
		for i, off := range offsets {
			values[i] = base + uint32(off)
		}

		codec, err := encodeFoR(array.NewPrimitivesUnsafe(values), testBuildBudget)
		if err != nil {
			return
		}

		assertRoundTrip(t, codec, values)
	})
}
