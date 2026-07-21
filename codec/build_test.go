package codec

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/axiomhq/btrblocks/array"
)

func TestExportedBuildersRejectInvalidInputs(t *testing.T) {
	t.Run("nil raw source", func(t *testing.T) {
		var source array.Array[uint64]
		if _, err := EncodeRaw(source); err == nil || !strings.Contains(err.Error(), "nil") {
			t.Fatalf("EncodeRaw error = %v, want nil-source error", err)
		}
	})

	t.Run("nil child", func(t *testing.T) {
		source := array.NewPrimitivesUnsafe([]uint64{1, 2})
		_, err := EncodeDelta(source, func(array.ArrayCore[uint64]) (EncodedArray[uint64], error) {
			return nil, nil
		})
		if err == nil || !strings.Contains(err.Error(), "nil child") {
			t.Fatalf("EncodeDelta error = %v, want nil-child error", err)
		}
	})

	t.Run("resized child", func(t *testing.T) {
		source := array.NewPrimitivesUnsafe([]uint64{1, 2, 3})
		_, err := EncodeDelta(source, func(array.ArrayCore[uint64]) (EncodedArray[uint64], error) {
			return newRawArray(array.NewPrimitivesUnsafe([]uint64{99})), nil
		})
		if err == nil || !strings.Contains(err.Error(), "child length = 1, want 2") {
			t.Fatalf("EncodeDelta error = %v, want child-length error", err)
		}
	})

	t.Run("nullable child", func(t *testing.T) {
		source := array.NewPrimitivesUnsafe([]uint64{1, 2})
		_, err := EncodeDelta(source, func(array.ArrayCore[uint64]) (EncodedArray[uint64], error) {
			validity, err := array.NewValidityUnsafe(1, []byte{0})
			if err != nil {
				return nil, err
			}
			values, err := array.NewPrimitivesWithValidityUnsafe([]uint64{1}, validity)
			if err != nil {
				return nil, err
			}
			return newRawArray(values), nil
		})
		if err == nil || !strings.Contains(err.Error(), "contains nulls") {
			t.Fatalf("EncodeDelta error = %v, want nullable-child error", err)
		}
	})

	t.Run("undersized run index", func(t *testing.T) {
		source := array.NewVirtual(257, func(i uint64) uint64 { return i })
		_, err := EncodePrimitiveRunEndAs(
			source,
			func(child array.ArrayCore[uint64]) (EncodedArray[uint64], error) { return testRawChild(child) },
			func(child array.ArrayCore[uint8]) (EncodedArray[uint8], error) { return testRawChild(child) },
		)
		if err == nil || !strings.Contains(err.Error(), "cannot represent") {
			t.Fatalf("EncodePrimitiveRunEndAs error = %v, want index-width error", err)
		}
	})

	t.Run("excessive declared length", func(t *testing.T) {
		source := repeatedArrayCore[uint64]{length: uint64(^uint(0)>>1) + 1, value: 1}
		if _, err := EncodeBitpack(source); !errors.Is(err, ErrMaterializationLimit) {
			t.Fatalf("EncodeBitpack error = %v, want %v", err, ErrMaterializationLimit)
		}
	})

	t.Run("configurable materialization budget", func(t *testing.T) {
		source := array.NewVirtual(2, func(i uint64) uint64 { return i + 1 })
		if _, err := EncodeFoR(source, BuildOptions{MaxBytes: 8}); !errors.Is(err, ErrMaterializationLimit) {
			t.Fatalf("EncodeFoR small-budget error = %v, want %v", err, ErrMaterializationLimit)
		}
		if _, err := EncodeFoR(source, BuildOptions{MaxBytes: 16}); err != nil {
			t.Fatalf("EncodeFoR sufficient-budget error = %v", err)
		}
	})

	t.Run("default budget rejects hostile FoR length before access", func(t *testing.T) {
		source := array.NewVirtual(DefaultMaxBuildBytes/8+1, func(uint64) uint64 {
			t.Fatal("EncodeFoR accessed a source that exceeds its build budget")
			return 0
		})
		if _, err := EncodeFoR(source); !errors.Is(err, ErrMaterializationLimit) {
			t.Fatalf("EncodeFoR error = %v, want %v", err, ErrMaterializationLimit)
		}
	})

	t.Run("run-end checks wider index allocation", func(t *testing.T) {
		source := array.NewPrimitivesUnsafe([]uint8{0, 1, 2, 3, 4, 5, 6, 7})
		failChild := func(array.ArrayCore[uint8]) (EncodedArray[uint8], error) {
			t.Fatal("run-end invoked a child after exceeding its allocation budget")
			return nil, nil
		}
		_, err := EncodePrimitiveRunEndAs(
			source,
			failChild,
			func(array.ArrayCore[uint64]) (EncodedArray[uint64], error) {
				t.Fatal("run-end invoked an index child after exceeding its allocation budget")
				return nil, nil
			},
			BuildOptions{MaxBytes: 8},
		)
		if !errors.Is(err, ErrMaterializationLimit) {
			t.Fatalf("EncodePrimitiveRunEndAs error = %v, want %v", err, ErrMaterializationLimit)
		}
	})

	t.Run("sparse checks wider index allocation", func(t *testing.T) {
		source := array.NewPrimitivesUnsafe([]uint8{1, 2, 3, 4, 5, 6, 7, 8})
		_, err := EncodeIntegerSparseWithFill(source, 0, testSparseChildren[uint8]{}, BuildOptions{MaxBytes: 8})
		if !errors.Is(err, ErrMaterializationLimit) {
			t.Fatalf("EncodeIntegerSparseWithFill error = %v, want %v", err, ErrMaterializationLimit)
		}
	})

	t.Run("dictionary bounds cardinality map", func(t *testing.T) {
		values := make([]float32, 100)
		for i := range values {
			values[i] = float32(i)
		}
		source := array.NewPrimitivesUnsafe(values)
		_, err := EncodeFloat32Dict(source, 100, testDictionaryChildren[float32]{}, BuildOptions{MaxBytes: 400})
		if !errors.Is(err, ErrMaterializationLimit) {
			t.Fatalf("EncodeFloat32Dict error = %v, want %v", err, ErrMaterializationLimit)
		}
	})

	t.Run("sparse bounds frequency map", func(t *testing.T) {
		source := array.NewVirtual(16, func(i uint64) uint8 { return uint8(i) })
		_, err := EncodeIntegerSparse(source, testSparseChildren[uint8]{}, BuildOptions{MaxBytes: 16})
		if !errors.Is(err, ErrMaterializationLimit) {
			t.Fatalf("EncodeIntegerSparse error = %v, want %v", err, ErrMaterializationLimit)
		}
	})

	t.Run("ALP rejects high patch ratio during collection", func(t *testing.T) {
		source := array.NewPrimitivesUnsafe([]float32{
			float32(math.NaN()), float32(math.NaN()), float32(math.NaN()), float32(math.NaN()),
		})
		_, err := EncodeALP32(source, testALPChildren{}, BuildOptions{MaxBytes: 16})
		if !errors.Is(err, ErrALPHighPatchRatio) {
			t.Fatalf("EncodeALP32 error = %v, want %v", err, ErrALPHighPatchRatio)
		}
	})

	t.Run("FSST checks concrete build allocations", func(t *testing.T) {
		source, err := array.NewStrings([]string{"01234567890123456789"})
		if err != nil {
			t.Fatal(err)
		}
		_, err = EncodeFSST(source, testUnsignedChildren{}, testUnsignedChildren{}, BuildOptions{MaxBytes: 24})
		if !errors.Is(err, ErrMaterializationLimit) {
			t.Fatalf("EncodeFSST error = %v, want %v", err, ErrMaterializationLimit)
		}
	})

	t.Run("unset child closure", func(t *testing.T) {
		source, err := array.NewStrings([]string{"abc"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := EncodeFSST(source, UnsignedChildFuncs{}, UnsignedChildFuncs{}); !errors.Is(err, ErrBuilderRequired) {
			t.Fatalf("EncodeFSST error = %v, want %v", err, ErrBuilderRequired)
		}
	})

	t.Run("nullable source", func(t *testing.T) {
		validity, err := array.NewValidityUnsafe(2, []byte{1})
		if err != nil {
			t.Fatal(err)
		}
		source, err := array.NewPrimitivesWithValidityUnsafe([]uint64{1, 2}, validity)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := EncodeBitpack(source); err == nil || !strings.Contains(err.Error(), "nulls") {
			t.Fatalf("EncodeBitpack error = %v, want nullable-source error", err)
		}
	})
}

// TestNewNullableScansValiditySequentially pins the bitmap check to a single
// sequential decode. The validity argument is an arbitrary codec tree, and
// deltaArray.ValueAt costs O(offset), so walking the bitmap with ValueAt made
// building one nullable column O((n/8)^2): a 1M-row bitmap is 128 KiB of bytes,
// each of which the walk reached by summing every byte before it.
func TestNewNullableScansValiditySequentially(t *testing.T) {
	const length = 1 << 20
	bitmap := make([]uint8, length/8)
	for i := range bitmap {
		bitmap[i] = 0xff
	}
	bitmap[0] = 0xfe // row 0 is the single null

	validity, err := EncodeDelta(array.NewPrimitivesUnsafe(bitmap), testRawChild[uint8])
	if err != nil {
		t.Fatalf("EncodeDelta error = %v", err)
	}
	values, err := NewConstArray(length, array.NewPrimitivesUnsafe([]uint64{7}))
	if err != nil {
		t.Fatalf("NewConstArray error = %v", err)
	}

	var nullable EncodedArray[uint64]
	err = assertAnsweredWithin(t, func() error {
		var err error
		nullable, err = NewNullable(values, validity, 1)
		return err
	})
	if err != nil {
		t.Fatalf("NewNullable error = %v", err)
	}
	if got := nullable.NullCount(); got != 1 {
		t.Fatalf("NullCount = %d, want 1", got)
	}
	if nullable.IsValid(0) {
		t.Fatal("IsValid(0) = true, want false")
	}
}

// assertBitExact decompresses encoded and compares every element with want
// under a bit-exact comparator. Encode* enforces only the O(1) half of the
// ChildBuilder contract, so the value half is verified here.
func assertBitExact[T Integer | Float | String](t *testing.T, encoded EncodedArray[T], want []T, equal cmpFn[T]) {
	t.Helper()
	if encoded.Length() != uint64(len(want)) {
		t.Fatalf("length = %d, want %d", encoded.Length(), len(want))
	}
	got := make([]T, len(want))
	if err := encoded.DecompressInto(got); err != nil {
		t.Fatalf("decompress: %v", err)
	}
	for i := range want {
		if !equal(want[i], got[i]) {
			t.Fatalf("value %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestExportedBuildersAreBitExact(t *testing.T) {
	negZero := math.Copysign(0, -1)

	t.Run("delta", func(t *testing.T) {
		values := []uint64{1, 2, 4, 8, 16}
		encoded, err := EncodeDelta(array.NewPrimitivesUnsafe(values), testRawChild[uint64])
		if err != nil {
			t.Fatalf("EncodeDelta error = %v", err)
		}
		assertBitExact(t, encoded, values, array.CmpIntegers[uint64])
	})

	t.Run("integer dictionary", func(t *testing.T) {
		values := []uint64{7, 3, 7, 3, 9}
		encoded, err := EncodeIntegerDict(array.NewPrimitivesUnsafe(values), testDictionaryChildren[uint64]{})
		if err != nil {
			t.Fatalf("EncodeIntegerDict error = %v", err)
		}
		assertBitExact(t, encoded, values, array.CmpIntegers[uint64])
	})

	t.Run("float dictionary keeps signed zero", func(t *testing.T) {
		values := []float64{0, negZero, 0, negZero}
		encoded, err := EncodeFloat64Dict(array.NewPrimitivesUnsafe(values), 0, testDictionaryChildren[float64]{})
		if err != nil {
			t.Fatalf("EncodeFloat64Dict error = %v", err)
		}
		assertBitExact(t, encoded, values, array.CmpFloatBits[float64])
	})

	t.Run("float sparse keeps signed zero", func(t *testing.T) {
		values := []float64{0, 0, negZero, 0}
		encoded, err := EncodeFloatSparse(array.NewPrimitivesUnsafe(values), testSparseChildren[float64]{})
		if err != nil {
			t.Fatalf("EncodeFloatSparse error = %v", err)
		}
		assertBitExact(t, encoded, values, array.CmpFloatBits[float64])
	})

	t.Run("float run-end keeps signed zero", func(t *testing.T) {
		values := []float64{0, negZero, negZero, 0}
		encoded, err := EncodePrimitiveRunEndAs(array.NewPrimitivesUnsafe(values), testRawChild[float64], testRawChild[uint8])
		if err != nil {
			t.Fatalf("EncodePrimitiveRunEndAs error = %v", err)
		}
		assertBitExact(t, encoded, values, array.CmpFloatBits[float64])
	})

	// array.Float is ~float32|~float64, so a defined type is as much a member
	// of the constraint as float64 itself and must get the same bit-exact
	// comparator. Dispatching on the predeclared name silently merged these
	// four values into one run of +0.0.
	t.Run("defined float type run-end keeps signed zero", func(t *testing.T) {
		type celsius float64
		values := []celsius{0, celsius(negZero), celsius(negZero), 0}
		encoded, err := EncodePrimitiveRunEndAs(array.NewPrimitivesUnsafe(values), testRawChild[celsius], testRawChild[uint8])
		if err != nil {
			t.Fatalf("EncodePrimitiveRunEndAs error = %v", err)
		}
		assertBitExact(t, encoded, values, array.CmpFloatBits[celsius])
	})

	t.Run("string dictionary", func(t *testing.T) {
		values := []string{"alpha", "beta", "alpha", "gamma"}
		encoded, err := EncodeStringDict(mustStrings(t, values), testStringDictionaryChildren{})
		if err != nil {
			t.Fatalf("EncodeStringDict error = %v", err)
		}
		assertBitExact(t, encoded, values, array.CmpStrings[string])
	})

	t.Run("fsst through child closures", func(t *testing.T) {
		values := []string{"aaaaabbbbb", "aaaaabbbbb", "cccccddddd"}
		children := UnsignedChildFuncs{
			Uint8:  testRawChild[uint8],
			Uint16: testRawChild[uint16],
			Uint32: testRawChild[uint32],
			Uint64: testRawChild[uint64],
		}
		encoded, err := EncodeFSST(mustStrings(t, values), children, children)
		if err != nil {
			t.Fatalf("EncodeFSST error = %v", err)
		}
		assertBitExact(t, encoded, values, array.CmpStrings[string])
	})
}
