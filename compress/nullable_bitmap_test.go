package compress

import (
	"testing"

	"github.com/axiomhq/btrblocks/array"
)

type overridingPrimitiveUint64Array struct {
	*array.Primitives[uint64]
	nulls     []bool
	nullCount uint64
}

func (a *overridingPrimitiveUint64Array) IsValid(offset uint64) bool {
	return !a.nulls[offset]
}

func (a *overridingPrimitiveUint64Array) NullCount() uint64 { return a.nullCount }

func TestMaskedPrimitiveBorrowsNativeValidity(t *testing.T) {
	validity, err := array.ValidityFromNulls(3, []bool{false, true, false})
	if err != nil {
		t.Fatalf("ValidityFromNulls: %v", err)
	}
	source, err := array.NewPrimitivesWithValidityUnsafe([]uint64{10, 999, 20}, validity)
	if err != nil {
		t.Fatalf("NewPrimitivesWithValidityUnsafe: %v", err)
	}
	masked := maskPrimitiveArray[uint64](source)
	if masked.validity == nil {
		t.Fatal("native primitive validity was not borrowed")
	}

	if got := masked.ValueAt(0); got != 10 {
		t.Fatalf("ValueAt(0) = %d, want 10", got)
	}
	if got := masked.ValueAt(1); got != 0 {
		t.Fatalf("ValueAt(1) = %d, want zero", got)
	}
	if got := masked.ValueAt(2); got != 20 {
		t.Fatalf("ValueAt(2) = %d, want 20", got)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("ValueAt(length) did not panic")
		}
	}()
	masked.ValueAt(masked.Length())
}

func TestMaskedPrimitiveUsesIsValidForWrapper(t *testing.T) {
	providerValidity, err := array.ValidityFromNulls(3, []bool{true, false, false})
	if err != nil {
		t.Fatalf("ValidityFromNulls: %v", err)
	}
	base, err := array.NewPrimitivesWithValidityUnsafe([]uint64{10, 999, 20}, providerValidity)
	if err != nil {
		t.Fatalf("NewPrimitivesWithValidityUnsafe: %v", err)
	}
	source := &overridingPrimitiveUint64Array{
		Primitives: base,
		nulls:      []bool{false, true, false},
		nullCount:  1,
	}
	if _, ok := any(source).(interface{ Validity() array.Validity }); !ok {
		t.Fatal("test wrapper does not promote Validity")
	}
	if bitmap := array.PrimitiveValidityBytes[uint64](source); bitmap != nil {
		t.Fatalf("wrapper primitive bitmap = %x, want nil", bitmap)
	}
	masked := maskPrimitiveArray[uint64](source)
	if masked.validity != nil {
		t.Fatal("wrapper validity was borrowed instead of using IsValid")
	}
	if got := masked.ValueAt(0); got != 10 {
		t.Fatalf("ValueAt(0) = %d, want 10", got)
	}
	if got := masked.ValueAt(1); got != 0 {
		t.Fatalf("ValueAt(1) = %d, want zero", got)
	}
	if got := masked.ValueAt(2); got != 20 {
		t.Fatalf("ValueAt(2) = %d, want 20", got)
	}
}
