package codec

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

func buildFSSTTest(arr array.ArrayCore[string]) (EncodedArray[string], error) {
	return encodeFSST(arr, testUnsignedChildren{}, testUnsignedChildren{}, testBuildBudget)
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

// TestFSSTDecodedBytesCountsStringHeaders pins the header term: DecompressInto
// fills a []string, so every element costs a string header whether or not it
// carries any payload. Summing only the payload reported 0 bytes for a
// 33M-element array and let Decompress allocate 512 MiB. The offsets and
// lengths children are counted too: DecompressInto decodes both before it
// touches a string.
//
// The allocation bound is the regression assertion: an undercounting
// DecodedBytes is not a wrong number in a report, it is half a gigabyte handed
// out on the way to an error.
func TestFSSTDecodedBytesCountsStringHeaders(t *testing.T) {
	const length = 1 << 25
	empty := &fsstArray[uint32, uint8]{
		denseRows: length,
		offsets:   newRawArray(array.NewPrimitivesUnsafe([]uint32{0})),
		lengths:   newRawArray(array.NewPrimitivesUnsafe([]uint8{0})),
	}

	got, err := empty.DecodedBytes()
	require.NoError(t, err)
	headers, err := decodedBytesFor(length, array.PTypeString)
	require.NoError(t, err)
	const childBytes = 4 + 1
	require.Equal(t, headers+childBytes, got)

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err = Decompress[string](empty)
	runtime.ReadMemStats(&after)
	require.ErrorIs(t, err, ErrMaterializationLimit)
	require.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(16<<20), "rejected, but only after allocating the array")
}

// TestFSSTDecodedBytesCountsDecodeBuffer covers the other buffer DecompressInto
// holds live for the whole fill: the reusable decode buffer, sized to the eight
// bytes per code byte that FSST can expand to. Poorly compressible input makes
// it several times the payload, and the read path caps a single span only at
// MaxDecodedBytes/8 — so leaving it out of a whole-subtree number hid up to a
// full decode limit per FSST node.
func TestFSSTDecodedBytesCountsDecodeBuffer(t *testing.T) {
	var builder strings.Builder
	for i := range 1 << 16 {
		builder.WriteByte(byte(i * 37))
	}
	values := []string{builder.String(), "b"}

	built, err := buildFSSTTest(mustStrings(t, values))
	require.NoError(t, err)
	loaded, err := LoadStrings(mustWriteEncodedArray(t, built))
	require.NoError(t, err)

	builtBytes, err := built.DecodedBytes()
	require.NoError(t, err)
	loadedBytes, err := loaded.DecodedBytes()
	require.NoError(t, err)
	require.Equal(t, builtBytes, loadedBytes, "builder and reader must derive the same footprint")

	headers, err := decodedBytesFor(uint64(len(values)), array.PTypeString)
	require.NoError(t, err)
	payload := uint64(len(values[0]) + len(values[1]))
	require.Greater(t, builtBytes, headers+4*payload, "decode buffer is unaccounted")

	decoded, err := Decompress(loaded)
	require.NoError(t, err)
	require.Equal(t, values, decoded)
}
