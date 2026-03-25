package btrblocks

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSequenceRoundTripUint32Arithmetic(t *testing.T) {
	values := []uint32{1000, 1003, 1006, 1009, 1012, 1015}
	codec, err := newSequenceArray(buildArray(values))
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

func TestSequenceRoundTripInt64Descending(t *testing.T) {
	values := []int64{100, 95, 90, 85, 80, 75}
	codec, err := newSequenceArray(buildArray(values))
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readBack, err := Read[int64](&buf)
	require.NoError(t, err)

	decoded, err := Decompress(readBack)
	require.NoError(t, err)
	require.Equal(t, values, decoded)
}

func TestSequenceRoundTripInt32NegativeStep(t *testing.T) {
	values := []int32{0, -5, -10, -15, -20}
	codec, err := newSequenceArray(buildArray(values))
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readBack, err := Read[int32](&buf)
	require.NoError(t, err)

	decoded, err := Decompress(readBack)
	require.NoError(t, err)
	require.Equal(t, values, decoded)
}

func TestSequenceNonSequenceReturnsError(t *testing.T) {
	_, err := newSequenceArray(buildArray([]uint32{1, 2, 4, 8}))
	require.ErrorIs(t, err, errNotArithmeticSequence)
}

func TestSequenceSingleElementReturnsError(t *testing.T) {
	_, err := newSequenceArray(buildArray([]uint32{42}))
	require.ErrorIs(t, err, errNotArithmeticSequence)
}

func TestSequenceZeroStepReturnsError(t *testing.T) {
	_, err := newSequenceArray(buildArray([]uint32{5, 5, 5}))
	require.ErrorIs(t, err, errNotArithmeticSequence)
}

func TestSequenceEncoding(t *testing.T) {
	codec, err := newSequenceArray(buildArray([]uint32{10, 20, 30}))
	require.NoError(t, err)
	require.Equal(t, CodecTypeSequence, codec.Encoding())
}

func TestSequenceValueAt(t *testing.T) {
	values := []uint32{1000, 1003, 1006, 1009, 1012, 1015}
	codec, err := newSequenceArray(buildArray(values))
	require.NoError(t, err)

	for i, v := range values {
		require.Equal(t, v, codec.ValueAt(uint64(i)))
	}
}

func TestSequenceValueAtAfterRead(t *testing.T) {
	values := []int64{100, 95, 90, 85, 80, 75}
	codec, err := newSequenceArray(buildArray(values))
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readBack, err := Read[int64](&buf)
	require.NoError(t, err)

	for i, v := range values {
		require.Equal(t, v, readBack.ValueAt(uint64(i)))
	}
}

func TestSequenceSliceShiftsBase(t *testing.T) {
	values := []uint32{10, 20, 30, 40, 50}
	codec, err := newSequenceArray(buildArray(values))
	require.NoError(t, err)

	sliced, err := codec.Slice(1, 4)
	require.NoError(t, err)
	require.Equal(t, uint64(3), sliced.Length())
	require.Equal(t, CodecTypeSequence, sliced.Encoding())

	decoded, err := Decompress(sliced)
	require.NoError(t, err)
	require.Equal(t, []uint32{20, 30, 40}, decoded)
}

func TestSequenceSliceAfterRead(t *testing.T) {
	values := []int32{0, -5, -10, -15, -20}
	codec, err := newSequenceArray(buildArray(values))
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readBack, err := Read[int32](&buf)
	require.NoError(t, err)

	sliced, err := readBack.Slice(1, 4)
	require.NoError(t, err)

	decoded, err := Decompress(sliced)
	require.NoError(t, err)
	require.Equal(t, []int32{-5, -10, -15}, decoded)
}

func BenchmarkSequence(b *testing.B) {
	for _, n := range []int{1_000, 10_000, 100_000, 1_000_000} {
		values := make([]uint32, n)
		for i := range values {
			values[i] = 1_000_000 + uint32(i)*3
		}

		b.Run(fmt.Sprintf("compress/%d", n), func(b *testing.B) {
			arr := buildArray(values)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := Compress(arr, Options{})
				if err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run(fmt.Sprintf("decompress/%d", n), func(b *testing.B) {
			arr := buildArray(values)
			encoded, err := Compress(arr, Options{})
			if err != nil {
				b.Fatal(err)
			}
			var buf bytes.Buffer
			_, err = encoded.WriteTo(&buf)
			if err != nil {
				b.Fatal(err)
			}
			data := buf.Bytes()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				r := bytes.NewReader(data)
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

func FuzzSequence(f *testing.F) {
	var seed bytes.Buffer
	_ = binary.Write(&seed, binary.LittleEndian, int64(100))
	_ = binary.Write(&seed, binary.LittleEndian, int64(3))
	f.Add(seed.Bytes())

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

		var buf bytes.Buffer
		_, err = codec.WriteTo(&buf)
		require.NoError(t, err)

		readBack, err := Read[int64](&buf)
		require.NoError(t, err)

		decoded, err := Decompress(readBack)
		require.NoError(t, err)
		require.Equal(t, values, decoded)
	})
}
