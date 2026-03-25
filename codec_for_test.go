package btrblocks

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

func TestFoRRoundTripUint32HighBase(t *testing.T) {
	values := []uint32{1000, 1002, 1004, 1006, 1008}
	codec, err := buildFoRArray(array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readBack, err := Read[uint32](&buf)
	require.NoError(t, err)

	decoded, err := Decompress(readBack)
	require.NoError(t, err)
	require.Equal(t, values, decoded)
}

func TestFoRRoundTripUint64NarrowRange(t *testing.T) {
	values := make([]uint64, 200)
	for i := range values {
		values[i] = 1_000_000 + uint64(i%100)
	}

	codec, err := buildFoRArray(array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readBack, err := Read[uint64](&buf)
	require.NoError(t, err)

	decoded, err := Decompress(readBack)
	require.NoError(t, err)
	require.Equal(t, values, decoded)
}

func TestFoREncodingIsCodecTypeFor(t *testing.T) {
	values := []uint32{1000, 1002, 1004, 1006, 1008}
	codec, err := buildFoRArray(array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)
	require.Equal(t, CodecTypeFor, codec.Encoding())
}

func TestFoRChildIsBitPacked(t *testing.T) {
	values := []uint32{1000, 1002, 1004, 1006, 1008}
	codec, err := buildFoRArray(array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	f, ok := codec.(*forArray[uint32])
	require.True(t, ok)
	require.IsType(t, &bitPackedArray[uint32, uint64]{}, f.child)
}

func TestFoRValueAtSpotChecks(t *testing.T) {
	values := []uint32{1000, 1002, 1004, 1006, 1008}
	codec, err := buildFoRArray(array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	for i, v := range values {
		require.Equal(t, v, codec.ValueAt(uint64(i)))
	}
}

func TestFoRSlicePreservesEncoding(t *testing.T) {
	values := []uint32{1000, 1002, 1004, 1006, 1008}
	codec, err := buildFoRArray(array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	sliced, err := codec.Slice(1, 4)
	require.NoError(t, err)
	require.Equal(t, CodecTypeFor, sliced.Encoding())
	require.Equal(t, uint64(3), sliced.Length())

	decoded, err := Decompress(sliced)
	require.NoError(t, err)
	require.Equal(t, values[1:4], decoded)
}

func BenchmarkFoRUint32(b *testing.B) {
	sizes := []int{1_000, 10_000, 100_000, 1_000_000}

	for _, n := range sizes {
		values := make([]uint32, n)
		for i := range values {
			values[i] = 1_000_000 + uint32(i%64)
		}

		b.Run(fmt.Sprintf("compress/%d", n), func(b *testing.B) {
			arr := buildArray(values)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				codec, err := Compress(arr, Options{})
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
			arr := buildArray(values)
			codec, err := Compress(arr, Options{})
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
				readBack, err := Read[uint32](r)
				if err != nil {
					b.Fatal(err)
				}
				_, err = Decompress(readBack)
				if err != nil {
					b.Fatal(err)
				}
			}
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

		codec, err := buildFoRArray(array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}))
		if err != nil {
			return
		}

		var buf bytes.Buffer
		_, err = codec.WriteTo(&buf)
		require.NoError(t, err)

		readBack, err := Read[uint32](&buf)
		require.NoError(t, err)

		decoded, err := Decompress(readBack)
		require.NoError(t, err)
		require.Equal(t, values, decoded)
	})
}
