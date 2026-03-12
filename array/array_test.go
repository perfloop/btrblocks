package array

import (
	"io"
	"testing"
)

const largeCorpusSize = 1 << 20

func TestArrayContractWithPrimitives(t *testing.T) {
	assertArrayContract(t, NewPrimitives([]uint16{3, 5, 8}), []uint16{3, 5, 8}, PTypeUint16, 6, 26)
}

func TestArrayContractWithStrings(t *testing.T) {
	assertArrayContract(t, NewStrings([]string{"go", "", "lang"}), []string{"go", "", "lang"}, PTypeString, 10, 34)
}

func assertArrayContract[T Integer | Float | String](t *testing.T, arr Array[T], want []T, wantPType PType, wantBinarySize uint64, wantWriteSize int64) {
	t.Helper()

	if got := arr.Length(); got != uint64(len(want)) {
		t.Fatalf("Length() = %d, want %d", got, len(want))
	}
	if got := arr.PType(); got != wantPType {
		t.Fatalf("PType() = %v, want %v", got, wantPType)
	}
	if got := arr.BinarySize(); got != wantBinarySize {
		t.Fatalf("BinarySize() = %d, want %d", got, wantBinarySize)
	}
	for i, wantValue := range want {
		if got := arr.ValueAt(uint64(i)); got != wantValue {
			t.Fatalf("ValueAt(%d) = %v, want %v", i, got, wantValue)
		}
	}
	n, err := arr.WriteTo(io.Discard)
	if err != nil {
		t.Fatalf("WriteTo() error = %v", err)
	}
	if n != wantWriteSize {
		t.Fatalf("WriteTo() bytes = %d, want %d", n, wantWriteSize)
	}
}
