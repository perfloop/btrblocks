package btrblocks

import (
	"bytes"
	"testing"

	"github.com/axiomhq/btrblocks/array"
)

func benchDecompress[T Integer | Float | String](b *testing.B, encoded EncodedArray[T]) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Decompress(encoded); err != nil {
			b.Fatal(err)
		}
	}
}

func benchDecompressInto[T Integer | Float | String](b *testing.B, encoded EncodedArray[T]) {
	dst := make([]T, encoded.Length())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := encoded.DecompressInto(dst); err != nil {
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

func makeRunEndUint32(n int) EncodedArray[uint32] {
	values := make([]uint32, n)
	for i := range values {
		values[i] = uint32(i / 20)
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

func BenchmarkDictDecompress_1K(b *testing.B)      { benchDecompress(b, makeDictUint32(1_000)) }
func BenchmarkDictDecompress_10K(b *testing.B)     { benchDecompress(b, makeDictUint32(10_000)) }
func BenchmarkDictDecompressInto_10K(b *testing.B) { benchDecompressInto(b, makeDictUint32(10_000)) }

func BenchmarkRunEndDecompress_1K(b *testing.B)      { benchDecompress(b, makeRunEndUint32(1_000)) }
func BenchmarkRunEndDecompress_10K(b *testing.B)     { benchDecompress(b, makeRunEndUint32(10_000)) }
func BenchmarkRunEndDecompressInto_10K(b *testing.B) { benchDecompressInto(b, makeRunEndUint32(10_000)) }

func BenchmarkALPDecompress_1K(b *testing.B)      { benchDecompress(b, makeALPFloat64(1_000)) }
func BenchmarkALPDecompress_10K(b *testing.B)     { benchDecompress(b, makeALPFloat64(10_000)) }
func BenchmarkALPDecompressInto_10K(b *testing.B) { benchDecompressInto(b, makeALPFloat64(10_000)) }

// serializeForRead compresses values and serializes to []byte for ReadBytes benchmarks.
func serializeForRead[T Integer | Float | String](values []T, opts Options) []byte {
	codec, err := Compress(buildArray(values), opts)
	if err != nil {
		panic(err)
	}
	var buf bytes.Buffer
	if _, err := codec.WriteTo(&buf); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func BenchmarkReadBytes_Dict_10K(b *testing.B) {
	values := make([]uint32, 10_000)
	for i := range values {
		values[i] = uint32(i % 50)
	}
	data := serializeForRead(values, Options{})
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := ReadBytes[uint32](data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReadBytes_RunEnd_10K(b *testing.B) {
	values := make([]uint32, 10_000)
	for i := range values {
		values[i] = uint32(i / 20)
	}
	data := serializeForRead(values, Options{})
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := ReadBytes[uint32](data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReadBytes_Raw_10K(b *testing.B) {
	values := make([]uint32, 10_000)
	for i := range values {
		values[i] = uint32(i)
	}
	data := serializeForRead(values, Options{})
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := ReadBytes[uint32](data); err != nil {
			b.Fatal(err)
		}
	}
}
