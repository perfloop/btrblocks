package btrblocks

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/axiomhq/btrblocks/array"
)

func BenchmarkSelectBest(b *testing.B) {
	for _, size := range []int{64_000, 1_000_000} {
		// Low-cardinality interleaved data — Dict should win.
		numbers := []int32{0, 123400, 617000, 1234000, 12340000, 37020000}
		data := make([]int32, size)
		for i := range data {
			data[i] = numbers[(i*7+3)%len(numbers)]
		}
		arr := array.NewPrimitivesUnsafe(data)
		label := fmt.Sprintf("%dK", size/1000)

		b.Run("sampled/"+label, func(b *testing.B) {
			builders := append(integerBaseBuilders[int32](),
				taggedBuilder[int32]{kind: CodecTypeZigzag, build: func(arr array.Array[int32], depth int, excl codecExcludes) (Codec[int32], error) {
					return NewZigzagCodec(arr, depth, excl)
				}})
			builders = append(builders, integerTailBuilders[int32]()...)
			stats := computeSignedIntStats(arr)
			b.ResetTimer()
			for range b.N {
				selectBest(arr, defaultDepth, builders, 0, stats.baseStats)
			}
		})

		b.Run("full/"+label, func(b *testing.B) {
			builders := append(integerBaseBuilders[int32](),
				taggedBuilder[int32]{kind: CodecTypeZigzag, build: func(arr array.Array[int32], depth int, excl codecExcludes) (Codec[int32], error) {
					return NewZigzagCodec(arr, depth, excl)
				}})
			builders = append(builders, integerTailBuilders[int32]()...)
			b.ResetTimer()
			for range b.N {
				selectBestAll(arr, defaultDepth, builders, 0)
			}
		})
	}
}

func TestCompressionRatios(t *testing.T) {
	const size = 1 << 20

	logRatio := func(name string, rawSize int, compressed uint64, codec any) {
		ratio := float64(rawSize) / float64(compressed)
		t.Logf("%-20s %10d B %10d B %8.1fx  %T", name, rawSize, compressed, ratio, codec)
	}

	t.Logf("\n%-20s %12s %12s %10s %s", "Pattern", "Raw", "Compressed", "Ratio", "Codec")
	t.Logf("%-20s %12s %12s %10s %s", "-------", "---", "----------", "-----", "-----")

	{
		data := make([]int32, size)
		for i := range data {
			data[i] = 42
		}
		c := Compress(data)
		logRatio("constant/int32", size*4, c.BinarySize(), c)
	}
	{
		numbers := []int32{0, 123400, 617000, 1234000, 12340000, 37020000}
		data := make([]int32, size)
		for i := range data {
			data[i] = numbers[(i*7+3)%len(numbers)]
		}
		c := Compress(data)
		logRatio("dict/int32", size*4, c.BinarySize(), c)
	}
	{
		data := make([]uint32, size)
		for i := range data {
			data[i] = 1_000_000 + uint32(i%1000)
		}
		c := Compress(data)
		logRatio("for/uint32", size*4, c.BinarySize(), c)
	}
	{
		data := make([]uint32, size)
		for i := range data {
			if i%100 == 0 {
				data[i] = uint32(i)
			} else {
				data[i] = 42
			}
		}
		c := Compress(data)
		logRatio("sparse/uint32", size*4, c.BinarySize(), c)
	}
	{
		data := make([]int64, size)
		for i := range data {
			data[i] = int64(i / 100)
		}
		c := Compress(data)
		logRatio("runend/int64", size*8, c.BinarySize(), c)
	}
	{
		data := make([]int32, size)
		for i := range data {
			data[i] = int32(i%256) - 128
		}
		c := Compress(data)
		logRatio("zigzag/int32", size*4, c.BinarySize(), c)
	}
	{
		distinct := []string{"apple", "banana", "cherry", "date", "elderberry"}
		data := make([]string, size)
		for i := range data {
			data[i] = distinct[i%len(distinct)]
		}
		c := Compress(data)
		logRatio("string/dict", size*7, c.BinarySize(), c)
	}
}

func BenchmarkCompressPipeline(b *testing.B) {
	const size = 1 << 20

	b.Run("constant/int32", func(b *testing.B) {
		data := make([]int32, size)
		for i := range data {
			data[i] = 42
		}
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			Compress(data)
		}
	})

	b.Run("dict/int32", func(b *testing.B) {
		numbers := []int32{0, 123400, 617000, 1234000, 12340000, 37020000}
		data := make([]int32, size)
		for i := range data {
			data[i] = numbers[(i*7+3)%len(numbers)]
		}
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			Compress(data)
		}
	})

	b.Run("for/uint32", func(b *testing.B) {
		data := make([]uint32, size)
		for i := range data {
			data[i] = 1_000_000 + uint32(i%1000)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			Compress(data)
		}
	})

	b.Run("sparse/uint32", func(b *testing.B) {
		data := make([]uint32, size)
		for i := range data {
			if i%100 == 0 {
				data[i] = uint32(i)
			} else {
				data[i] = 42
			}
		}
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			Compress(data)
		}
	})

	b.Run("runend/int64", func(b *testing.B) {
		data := make([]int64, size)
		for i := range data {
			data[i] = int64(i / 100)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			Compress(data)
		}
	})

	b.Run("zigzag/int32", func(b *testing.B) {
		data := make([]int32, size)
		for i := range data {
			data[i] = int32(i%256) - 128
		}
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			Compress(data)
		}
	})

	b.Run("string/dict", func(b *testing.B) {
		distinct := []string{"apple", "banana", "cherry", "date", "elderberry"}
		data := make([]string, size)
		for i := range data {
			data[i] = distinct[i%len(distinct)]
		}
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			Compress(data)
		}
	})
}

func BenchmarkDecompressPipeline(b *testing.B) {
	const size = 1 << 20

	b.Run("for/uint32", func(b *testing.B) {
		data := make([]uint32, size)
		for i := range data {
			data[i] = 1_000_000 + uint32(i%1000)
		}
		codec := Compress(data)
		var buf bytes.Buffer
		_, _ = codec.WriteTo(&buf)
		encoded := buf.Bytes()

		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			_, _ = Decompress[uint32](bytes.NewReader(encoded))
		}
	})

	b.Run("sparse/uint32", func(b *testing.B) {
		data := make([]uint32, size)
		for i := range data {
			if i%100 == 0 {
				data[i] = uint32(i)
			} else {
				data[i] = 42
			}
		}
		codec := Compress(data)
		var buf bytes.Buffer
		_, _ = codec.WriteTo(&buf)
		encoded := buf.Bytes()

		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			_, _ = Decompress[uint32](bytes.NewReader(encoded))
		}
	})

	b.Run("dict/int32", func(b *testing.B) {
		numbers := []int32{0, 123400, 617000, 1234000, 12340000, 37020000}
		data := make([]int32, size)
		for i := range data {
			data[i] = numbers[(i*7+3)%len(numbers)]
		}
		codec := Compress(data)
		var buf bytes.Buffer
		_, _ = codec.WriteTo(&buf)
		encoded := buf.Bytes()

		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			_, _ = Decompress[int32](bytes.NewReader(encoded))
		}
	})
}
