package codec

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSequenceRoundTrip(t *testing.T) {
	t.Run("uint32_arithmetic", func(t *testing.T) {
		values := []uint32{1000, 1003, 1006, 1009, 1012, 1015}
		codec, err := newSequenceArray(buildArray(values))
		require.NoError(t, err)
		assertRoundTrip(t, codec, values)
	})

	t.Run("int64_descending", func(t *testing.T) {
		values := []int64{100, 95, 90, 85, 80, 75}
		codec, err := newSequenceArray(buildArray(values))
		require.NoError(t, err)
		assertRoundTrip(t, codec, values)
	})

	t.Run("int32_negative_step", func(t *testing.T) {
		values := []int32{0, -5, -10, -15, -20}
		codec, err := newSequenceArray(buildArray(values))
		require.NoError(t, err)
		assertRoundTrip(t, codec, values)
	})
}

func TestSequenceErrors(t *testing.T) {
	t.Run("non_sequence", func(t *testing.T) {
		_, err := newSequenceArray(buildArray([]uint32{1, 2, 4, 8}))
		require.ErrorIs(t, err, errNotArithmeticSequence)
	})

	t.Run("single_element", func(t *testing.T) {
		_, err := newSequenceArray(buildArray([]uint32{42}))
		require.ErrorIs(t, err, errNotArithmeticSequence)
	})

	t.Run("zero_step", func(t *testing.T) {
		_, err := newSequenceArray(buildArray([]uint32{5, 5, 5}))
		require.ErrorIs(t, err, errNotArithmeticSequence)
	})
}

func TestSequenceEncoding(t *testing.T) {
	codec, err := newSequenceArray(buildArray([]uint32{10, 20, 30}))
	require.NoError(t, err)
	require.Equal(t, CodecTypeSequence, codec.CodecType())
}

func TestSequenceSlice(t *testing.T) {
	t.Run("shifts_base", func(t *testing.T) {
		values := []uint32{10, 20, 30, 40, 50}
		codec, err := newSequenceArray(buildArray(values))
		require.NoError(t, err)
		assertSliceRoundTrip(t, codec, 1, 4, values)
	})

	t.Run("after_read", func(t *testing.T) {
		values := []int32{0, -5, -10, -15, -20}
		codec, err := newSequenceArray(buildArray(values))
		require.NoError(t, err)

		data := mustWriteEncodedArray(t, codec)
		readBack, err := LoadSigned[int32](data)
		require.NoError(t, err)

		assertSliceRoundTrip(t, readBack, 1, 4, values)
	})
}

func BenchmarkSequence(b *testing.B) {
	for _, n := range benchSizes {
		values := make([]uint32, n)
		for i := range values {
			values[i] = 1_000_000 + uint32(i)*3
		}

		b.Run(fmt.Sprintf("compress/%d", n), func(b *testing.B) {
			arr := buildArray(values)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := newSequenceArray(arr); err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run(fmt.Sprintf("decompress/%d", n), func(b *testing.B) {
			arr := buildArray(values)
			codec, err := newSequenceArray(arr)
			if err != nil {
				b.Fatal(err)
			}
			benchDecompress(b, codec)
		})

		b.Run(fmt.Sprintf("decompressInto/%d", n), func(b *testing.B) {
			arr := buildArray(values)
			codec, err := newSequenceArray(arr)
			if err != nil {
				b.Fatal(err)
			}
			benchDecompressInto(b, codec)
		})
	}
}

func FuzzSequence(f *testing.F) {
	var seed [16]byte
	binary.LittleEndian.PutUint64(seed[:8], uint64(100))
	binary.LittleEndian.PutUint64(seed[8:], uint64(3))
	f.Add(seed[:])

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 16 {
			return
		}
		base := int64(binary.LittleEndian.Uint64(data[:8]))
		step := int64(binary.LittleEndian.Uint64(data[8:16]))
		if step == 0 {
			return
		}

		const count = 100
		values := make([]int64, count)
		for i := range values {
			values[i] = base + int64(i)*step
		}

		codec, err := newSequenceArray(buildArray(values))
		require.NoError(t, err)

		assertRoundTrip(t, codec, values)
	})
}
