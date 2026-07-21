package codec

import (
	"errors"
	"testing"

	"github.com/axiomhq/btrblocks/array"
)

var benchSizes = []int{1_000, 10_000, 100_000, 1_000_000}

func TestDecompressRejectsLargeCompactOutput(t *testing.T) {
	data := mustWriteEncodedArray(t, &constArray[uint64]{denseRows: 1 << 26, body: array.NewPrimitivesUnsafe([]uint64{1})})
	encoded, err := LoadUnsigned[uint64](data)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := Decompress(encoded); !errors.Is(err, ErrMaterializationLimit) {
		t.Fatalf("Decompress error = %v, want %v", err, ErrMaterializationLimit)
	}
}

func benchDecompress[T Integer | Float | String](b *testing.B, encoded EncodedArray[T]) {
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := Decompress(encoded); err != nil {
			b.Fatal(err)
		}
	}
}

func benchDecompressInto[T Integer | Float | String](b *testing.B, encoded EncodedArray[T]) {
	dst := make([]T, encoded.Length())
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := encoded.DecompressInto(dst); err != nil {
			b.Fatal(err)
		}
	}
}
