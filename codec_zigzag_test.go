package btrblocks

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestZigZagRoundTripInt32Negatives(t *testing.T) {
	values := make([]int32, 256)
	for i := range values {
		if i%2 == 0 {
			values[i] = -7
		} else {
			values[i] = -3
		}
	}

	codec, err := buildZigZagArray(buildArray(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readBack, err := Read[int32](&buf)
	require.NoError(t, err)

	decoded, err := readBack.Decompress()
	require.NoError(t, err)
	require.Equal(t, values, decoded)
}

func TestZigZagRoundTripInt64Mixed(t *testing.T) {
	values := make([]int64, 256)
	for i := range values {
		if i%2 == 0 {
			values[i] = -100
		} else {
			values[i] = 50
		}
	}

	codec, err := buildZigZagArray(buildArray(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readBack, err := Read[int64](&buf)
	require.NoError(t, err)

	decoded, err := readBack.Decompress()
	require.NoError(t, err)
	require.Equal(t, values, decoded)
}

func TestZigZagRoundTripInt8SmallNegatives(t *testing.T) {
	values := make([]int8, 256)
	for i := range values {
		if i%2 == 0 {
			values[i] = -1
		} else {
			values[i] = -2
		}
	}

	codec, err := buildZigZagArray(buildArray(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readBack, err := Read[int8](&buf)
	require.NoError(t, err)

	decoded, err := readBack.Decompress()
	require.NoError(t, err)
	require.Equal(t, values, decoded)
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

	codec, err := buildZigZagArray(buildArray(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)
	require.Equal(t, CodecTypeZigZag, codec.Encoding())
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

	codec, err := buildZigZagArray(buildArray(values), newPlanContext(Options{MaxDepth: 3}))
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
	codec, err := Compress(buildArray(values), Options{})
	if err != nil {
		panic(err)
	}
	return codec
}

func BenchmarkZigZagCompress_1K(b *testing.B) {
	values := make([]int32, 1_000)
	for i := range values {
		values[i] = int32((i % 7) - 3)
	}
	arr := buildArray(values)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Compress(arr, Options{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkZigZagCompress_10K(b *testing.B) {
	values := make([]int32, 10_000)
	for i := range values {
		values[i] = int32((i % 7) - 3)
	}
	arr := buildArray(values)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Compress(arr, Options{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkZigZagCompress_100K(b *testing.B) {
	values := make([]int32, 100_000)
	for i := range values {
		values[i] = int32((i % 7) - 3)
	}
	arr := buildArray(values)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Compress(arr, Options{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkZigZagCompress_1M(b *testing.B) {
	values := make([]int32, 1_000_000)
	for i := range values {
		values[i] = int32((i % 7) - 3)
	}
	arr := buildArray(values)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Compress(arr, Options{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkZigZagDecompress_1K(b *testing.B)   { benchDecompress(b, makeZigZagInt32(1_000)) }
func BenchmarkZigZagDecompress_10K(b *testing.B)  { benchDecompress(b, makeZigZagInt32(10_000)) }
func BenchmarkZigZagDecompress_100K(b *testing.B) { benchDecompress(b, makeZigZagInt32(100_000)) }
func BenchmarkZigZagDecompress_1M(b *testing.B)   { benchDecompress(b, makeZigZagInt32(1_000_000)) }

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

		codec, err := Compress(buildArray(values), Options{})
		if err != nil {
			t.Fatal(err)
		}

		var buf bytes.Buffer
		_, err = codec.WriteTo(&buf)
		if err != nil {
			t.Fatal(err)
		}

		readBack, err := Read[int32](&buf)
		if err != nil {
			t.Fatal(err)
		}

		decoded, err := readBack.Decompress()
		if err != nil {
			t.Fatal(err)
		}

		require.Equal(t, values, decoded, fmt.Sprintf("roundtrip mismatch for %d elements", n))
	})
}
