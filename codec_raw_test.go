package btrblocks

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func TestRawRoundTripUint32(t *testing.T) {
	values := []uint32{1, 2, 3, 100, 200, 300}
	codec := newRawArray(buildArray(values))

	var buf bytes.Buffer
	_, err := codec.WriteTo(&buf)
	require.NoError(t, err)

	readBack, err := Read[uint32](&buf)
	require.NoError(t, err)

	decoded, err := Decompress(readBack)
	require.NoError(t, err)
	require.Equal(t, values, decoded)
}

func TestRawRoundTripInt32(t *testing.T) {
	values := []int32{-10, 0, 10, math.MinInt32, math.MaxInt32}
	codec := newRawArray(buildArray(values))

	var buf bytes.Buffer
	_, err := codec.WriteTo(&buf)
	require.NoError(t, err)

	readBack, err := Read[int32](&buf)
	require.NoError(t, err)

	decoded, err := Decompress(readBack)
	require.NoError(t, err)
	require.Equal(t, values, decoded)
}

func TestRawRoundTripFloat64(t *testing.T) {
	values := []float64{0.0, -1.5, 3.14, math.Inf(1), math.NaN()}
	codec := newRawArray(buildArray(values))

	var buf bytes.Buffer
	_, err := codec.WriteTo(&buf)
	require.NoError(t, err)

	readBack, err := Read[float64](&buf)
	require.NoError(t, err)

	decoded, err := Decompress(readBack)
	require.NoError(t, err)
	require.True(t, equalFloats(values, decoded))
}

func TestRawRoundTripString(t *testing.T) {
	values := []string{"hello", "", "world", "foo bar"}
	codec := newRawArray(buildArray(values))

	var buf bytes.Buffer
	_, err := codec.WriteTo(&buf)
	require.NoError(t, err)

	readBack, err := Read[string](&buf)
	require.NoError(t, err)

	decoded, err := Decompress(readBack)
	require.NoError(t, err)
	require.Equal(t, values, decoded)
}

func TestRawRoundTripUint64(t *testing.T) {
	values := []uint64{0, 1, math.MaxUint64, 42}
	codec := newRawArray(buildArray(values))

	var buf bytes.Buffer
	_, err := codec.WriteTo(&buf)
	require.NoError(t, err)

	readBack, err := Read[uint64](&buf)
	require.NoError(t, err)

	decoded, err := Decompress(readBack)
	require.NoError(t, err)
	require.Equal(t, values, decoded)
}

func TestRawValueAt(t *testing.T) {
	values := []uint32{10, 20, 30, 40, 50}
	codec := newRawArray(buildArray(values))

	for i, v := range values {
		require.Equal(t, v, codec.ValueAt(uint64(i)))
	}
}

func TestRawValueAtAfterRead(t *testing.T) {
	values := []int32{-5, 0, 5, 10}
	codec := newRawArray(buildArray(values))

	var buf bytes.Buffer
	_, err := codec.WriteTo(&buf)
	require.NoError(t, err)

	readBack, err := Read[int32](&buf)
	require.NoError(t, err)

	for i, v := range values {
		require.Equal(t, v, readBack.ValueAt(uint64(i)))
	}
}

func TestRawSlice(t *testing.T) {
	values := []uint32{10, 20, 30, 40, 50}
	codec := newRawArray(buildArray(values))

	sliced, err := codec.Slice(1, 4)
	require.NoError(t, err)
	require.Equal(t, uint64(3), sliced.Length())
	require.Equal(t, CodecTypeRaw, sliced.Encoding())

	decoded, err := Decompress(sliced)
	require.NoError(t, err)
	require.Equal(t, []uint32{20, 30, 40}, decoded)
}

func TestRawSliceAfterRead(t *testing.T) {
	values := []int32{-3, -2, -1, 0, 1, 2, 3}
	codec := newRawArray(buildArray(values))

	var buf bytes.Buffer
	_, err := codec.WriteTo(&buf)
	require.NoError(t, err)

	readBack, err := Read[int32](&buf)
	require.NoError(t, err)

	sliced, err := readBack.Slice(2, 5)
	require.NoError(t, err)

	decoded, err := Decompress(sliced)
	require.NoError(t, err)
	require.Equal(t, []int32{-1, 0, 1}, decoded)
}

func TestRawEncoding(t *testing.T) {
	codec := newRawArray(buildArray([]uint32{1, 2, 3}))
	require.Equal(t, CodecTypeRaw, codec.Encoding())
}

func BenchmarkRaw(b *testing.B) {
	for _, n := range []int{1_000, 10_000, 100_000, 1_000_000} {
		values := make([]uint32, n)
		for i := range values {
			values[i] = uint32(i)
		}
		arr := buildArray(values)

		b.Run(fmt.Sprintf("compress/%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				codec := newRawArray(arr)
				var buf bytes.Buffer
				_, _ = codec.WriteTo(&buf)
			}
		})

		b.Run(fmt.Sprintf("decompress/%d", n), func(b *testing.B) {
			codec := newRawArray(arr)
			var buf bytes.Buffer
			_, _ = codec.WriteTo(&buf)
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

func FuzzRaw(f *testing.F) {
	f.Add([]byte{1, 0, 0, 0, 2, 0, 0, 0, 3, 0, 0, 0})
	f.Add(make([]byte, 4))
	f.Add([]byte{0xff, 0xff, 0xff, 0xff})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 4 || len(data)%4 != 0 {
			return
		}
		n := len(data) / 4
		values := unsafe.Slice((*uint32)(unsafe.Pointer(&data[0])), n)

		owned := make([]uint32, n)
		copy(owned, values)

		codec := newRawArray(buildArray(owned))

		var buf bytes.Buffer
		_, err := codec.WriteTo(&buf)
		require.NoError(t, err)

		readBack, err := Read[uint32](&buf)
		require.NoError(t, err)

		decoded, err := Decompress(readBack)
		require.NoError(t, err)
		require.Equal(t, owned, decoded)
	})
}

func FuzzRawFloat64(f *testing.F) {
	var seed bytes.Buffer
	for _, v := range []float64{0.0, 1.5, -1.5, math.Inf(1)} {
		_ = binary.Write(&seed, binary.LittleEndian, v)
	}
	f.Add(seed.Bytes())

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 8 || len(data)%8 != 0 {
			return
		}
		n := len(data) / 8
		values := unsafe.Slice((*float64)(unsafe.Pointer(&data[0])), n)

		owned := make([]float64, n)
		copy(owned, values)

		codec := newRawArray(buildArray(owned))

		var buf bytes.Buffer
		_, err := codec.WriteTo(&buf)
		require.NoError(t, err)

		readBack, err := Read[float64](&buf)
		require.NoError(t, err)

		decoded, err := Decompress(readBack)
		require.NoError(t, err)
		require.True(t, equalFloats(owned, decoded))
	})
}
