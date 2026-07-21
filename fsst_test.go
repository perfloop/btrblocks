package btrblocks

import (
	"fmt"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

func buildFSSTTest(arr array.ArrayCore[string]) (EncodedArray[string], error) {
	return encodeFSST(arr, testUnsignedChildren{}, testUnsignedChildren{})
}

func TestFSSTRoundTrip(t *testing.T) {
	t.Run("repetitive strings", func(t *testing.T) {
		values := make([]string, 100)
		for i := range values {
			values[i] = fmt.Sprintf("http://example.com/page%d", i%10)
		}
		codec, err := buildFSSTTest(mustStrings(t, values))
		require.NoError(t, err)
		require.Equal(t, CodecTypeFSST, codec.CodecType())
		assertRoundTrip(t, codec, values)
	})
	t.Run("common prefixes", func(t *testing.T) {
		suffixes := []string{"aaa", "bbb", "ccc", "ddd", "eee"}
		values := make([]string, 50)
		for i := range values {
			values[i] = "prefix_" + suffixes[i%len(suffixes)]
		}
		codec, err := buildFSSTTest(mustStrings(t, values))
		require.NoError(t, err)
		assertRoundTrip(t, codec, values)
	})
	t.Run("empty strings mixed", func(t *testing.T) {
		values := []string{"hello", "", "world", "", "", "foo", "", "bar"}
		codec, err := buildFSSTTest(mustStrings(t, values))
		require.NoError(t, err)
		assertRoundTrip(t, codec, values)
	})
	t.Run("single char strings", func(t *testing.T) {
		values := make([]string, 50)
		for i := range values {
			values[i] = string(rune('a' + i%26))
		}
		codec, err := buildFSSTTest(mustStrings(t, values))
		require.NoError(t, err)
		assertRoundTrip(t, codec, values)
	})
}

func TestFSSTIndependentWidthNarrowing(t *testing.T) {
	// 1000 short strings (max len < 256 -> uint8 lengths) whose total
	// compressed size exceeds 256 bytes (-> uint16 or wider offsets).
	values := make([]string, 1000)
	for i := range values {
		values[i] = fmt.Sprintf("k%d", i%50)
	}

	codec, err := buildFSSTTest(mustStrings(t, values))
	require.NoError(t, err)

	f := codec.(*fsstArray[uint16, uint8])
	require.True(t, f != nil, "expected fsstArray[uint16, uint8]")

	decoded, err := Decompress(codec)
	require.NoError(t, err)
	require.Equal(t, values, decoded)
}

func makeFSSTBenchData(n int) []string {
	values := make([]string, n)
	paths := []string{"/api/v1/users", "/api/v1/orders", "/api/v1/products", "/api/v2/users", "/api/v2/orders"}
	for i := range values {
		values[i] = "https://example.com" + paths[i%len(paths)] + fmt.Sprintf("/%d", i)
	}
	return values
}

func BenchmarkFSST(b *testing.B) {
	for _, n := range benchSizes {
		values := makeFSSTBenchData(n)
		arr := mustStrings(b, values)

		b.Run(fmt.Sprintf("compress/%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				if _, err := buildFSSTTest(arr); err != nil {
					b.Fatal(err)
				}
			}
		})

		codec, err := buildFSSTTest(arr)
		if err != nil {
			b.Fatal(err)
		}

		b.Run(fmt.Sprintf("decompress/%d", n), func(b *testing.B) {
			benchDecompress(b, codec)
		})
		b.Run(fmt.Sprintf("decompressInto/%d", n), func(b *testing.B) {
			benchDecompressInto(b, codec)
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
			end := min(pos+chunkLen, len(data))
			values = append(values, string(data[pos:end]))
			pos = end
		}

		if len(values) == 0 {
			return
		}

		codec, err := buildFSSTTest(mustStrings(t, values))
		if err != nil {
			return
		}

		decoded, err := Decompress(codec)
		require.NoError(t, err)
		require.Equal(t, values, decoded)
	})
}
