package btrblocks

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFSSTRoundTripRepetitiveStrings(t *testing.T) {
	values := make([]string, 100)
	for i := range values {
		values[i] = fmt.Sprintf("http://example.com/page%d", i%10)
	}

	codec, err := buildFSSTArray(buildArray(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readBack, err := Load[string](buf.Bytes())
	require.NoError(t, err)

	decoded, err := Decompress(readBack)
	require.NoError(t, err)
	require.Equal(t, values, decoded)
}

func TestFSSTRoundTripCommonPrefixes(t *testing.T) {
	suffixes := []string{"aaa", "bbb", "ccc", "ddd", "eee"}
	values := make([]string, 50)
	for i := range values {
		values[i] = "prefix_" + suffixes[i%len(suffixes)]
	}

	codec, err := buildFSSTArray(buildArray(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readBack, err := Load[string](buf.Bytes())
	require.NoError(t, err)

	decoded, err := Decompress(readBack)
	require.NoError(t, err)
	require.Equal(t, values, decoded)
}

func TestFSSTRoundTripEmptyStringsMixed(t *testing.T) {
	values := []string{"hello", "", "world", "", "", "foo", "", "bar"}

	codec, err := buildFSSTArray(buildArray(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readBack, err := Load[string](buf.Bytes())
	require.NoError(t, err)

	decoded, err := Decompress(readBack)
	require.NoError(t, err)
	require.Equal(t, values, decoded)
}

func TestFSSTRoundTripSingleCharStrings(t *testing.T) {
	values := make([]string, 50)
	for i := range values {
		values[i] = string(rune('a' + i%26))
	}

	codec, err := buildFSSTArray(buildArray(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readBack, err := Load[string](buf.Bytes())
	require.NoError(t, err)

	decoded, err := Decompress(readBack)
	require.NoError(t, err)
	require.Equal(t, values, decoded)
}

func TestFSSTEncodingType(t *testing.T) {
	values := make([]string, 100)
	for i := range values {
		values[i] = fmt.Sprintf("http://example.com/page%d", i%10)
	}

	codec, err := buildFSSTArray(buildArray(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)
	require.Equal(t, CodecTypeFSST, codec.Encoding())
}

func TestFSSTValueAt(t *testing.T) {
	values := []string{"alpha", "beta", "gamma", "delta", "epsilon"}

	codec, err := buildFSSTArray(buildArray(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	for i, want := range values {
		require.Equal(t, want, codec.ValueAt(uint64(i)))
	}
}

func makeFSSTBenchData(n int) []string {
	values := make([]string, n)
	paths := []string{"/api/v1/users", "/api/v1/orders", "/api/v1/products", "/api/v2/users", "/api/v2/orders"}
	for i := range values {
		values[i] = "https://example.com" + paths[i%len(paths)] + fmt.Sprintf("/%d", i)
	}
	return values
}

func BenchmarkFSSTCompress(b *testing.B) {
	for _, n := range []int{1_000, 10_000, 100_000, 1_000_000} {
		values := makeFSSTBenchData(n)
		arr := buildArray(values)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := Compress(arr, Options{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkFSSTDecompress(b *testing.B) {
	for _, n := range []int{1_000, 10_000, 100_000, 1_000_000} {
		values := makeFSSTBenchData(n)
		codec, err := Compress(buildArray(values), Options{})
		if err != nil {
			b.Fatal(err)
		}
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := Decompress(codec); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func FuzzFSSTRoundTrip(f *testing.F) {
	f.Add([]byte("hello world this is a test of fsst compression"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 4 {
			return
		}

		var values []string
		pos := 0
		for pos < len(data) {
			chunkLen := int(data[pos]%16) + 1
			pos++
			end := pos + chunkLen
			if end > len(data) {
				end = len(data)
			}
			values = append(values, string(data[pos:end]))
			pos = end
		}

		if len(values) == 0 {
			return
		}

		codec, err := buildFSSTArray(buildArray(values), newPlanContext(Options{MaxDepth: 3}))
		if err != nil {
			return
		}

		decoded, err := Decompress(codec)
		require.NoError(t, err)
		require.Equal(t, values, decoded)
	})
}
