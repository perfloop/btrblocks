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

	t.Run("changed child value", func(t *testing.T) {
		source := array.NewPrimitivesUnsafe([]uint64{1, 2})
		_, err := EncodeDelta(source, func(array.ArrayCore[uint64]) (EncodedArray[uint64], error) {
			return newRawArray(array.NewPrimitivesUnsafe([]uint64{99})), nil
		})
		if err == nil || !strings.Contains(err.Error(), "value differs") {
			t.Fatalf("EncodeDelta error = %v, want changed-value error", err)
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
