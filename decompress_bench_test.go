package btrblocks

import (
	"testing"

	"github.com/axiomhq/btrblocks/array"
)

func benchDecompress[T Integer | Float | String](b *testing.B, encoded EncodedArray[T]) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := encoded.Decompress(); err != nil {
			b.Fatal(err)
		}
	}
}

func makeDictUint32(n int) EncodedArray[uint32] {
	values := make([]uint32, n)
	for i := range values {
		values[i] = uint32(i % 50)
	}
	codec, err := Compress(array.NewPrimitivesUnsafe(values), Options{})
	if err != nil {
		panic(err)
	}
	return codec
}

func makeALPFloat64(n int) EncodedArray[float64] {
	values := make([]float64, n)
	for i := range values {
		values[i] = 3.14 * float64(i%100)
	}
	codec, err := Compress(array.NewPrimitivesUnsafe(values), Options{})
	if err != nil {
		panic(err)
	}
	return codec
}

func BenchmarkDictDecompress_1K(b *testing.B)  { benchDecompress(b, makeDictUint32(1_000)) }
func BenchmarkDictDecompress_10K(b *testing.B) { benchDecompress(b, makeDictUint32(10_000)) }
func BenchmarkALPDecompress_1K(b *testing.B)   { benchDecompress(b, makeALPFloat64(1_000)) }
func BenchmarkALPDecompress_10K(b *testing.B)  { benchDecompress(b, makeALPFloat64(10_000)) }
