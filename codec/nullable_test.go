package codec

import (
	"strings"
	"testing"

	"github.com/axiomhq/btrblocks/array"
)

func requireErrorContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want containing %q", err, want)
	}
}

func TestReadRejectsNullableStructuralChild(t *testing.T) {
	validity, err := array.ValidityFromNulls(1, []bool{true})
	if err != nil {
		t.Fatalf("ValidityFromNulls: %v", err)
	}
	body, err := array.NewPrimitivesWithValidityUnsafe([]uint32{0}, validity)
	if err != nil {
		t.Fatalf("NewPrimitivesWithValidityUnsafe: %v", err)
	}
	encoded := &constArray[uint32]{denseRows: 4, body: body}

	_, err = LoadUnsigned[uint32](mustWriteEncodedArray(t, encoded))
	requireErrorContains(t, err, "const body child contains nulls")
}

func TestReadNullableRejectsInvalidValidity(t *testing.T) {
	constValues := func(length uint64) EncodedArray[uint64] {
		return &constArray[uint64]{
			denseRows: denseRows(length),
			body:      array.NewPrimitivesUnsafe([]uint64{0}),
		}
	}

	t.Run("unused tail bits", func(t *testing.T) {
		encoded := &nullableArray[uint64]{
			values:    constValues(65),
			validity:  newRawArray(array.NewPrimitivesUnsafe([]uint8{0, 0, 0, 0, 0, 0, 0, 0, 2})),
			nullCount: 64,
		}
		_, err := LoadUnsigned[uint64](mustWriteEncodedArray(t, encoded))
		requireErrorContains(t, err, "non-zero unused bits")
	})

	t.Run("null count mismatch", func(t *testing.T) {
		encoded := &nullableArray[uint64]{
			values: constValues(64),
			validity: &constArray[uint8]{
				denseRows: 8,
				body:      array.NewPrimitivesUnsafe([]uint8{^uint8(0)}),
			},
			nullCount: 1,
		}
		_, err := LoadUnsigned[uint64](mustWriteEncodedArray(t, encoded))
		requireErrorContains(t, err, "bitmap has 0 nulls, header says 1")
	})
}

func TestNewNullableRejectsOverflowedBitmapLength(t *testing.T) {
	values := &constArray[uint64]{
		denseRows: denseRows(^uint64(0)),
		body:      array.NewPrimitivesUnsafe([]uint64{0}),
	}
	validity := newRawArray(array.NewPrimitivesUnsafe([]uint8{}))
	if _, err := NewNullable(values, validity, ^uint64(0)); err == nil {
		t.Fatal("NewNullable accepted an overflow-sized empty validity child")
	}
}
