package btrblocks

import (
	"fmt"
	"strings"
	"testing"

	"github.com/axiomhq/btrblocks/array"
)

var (
	benchmarkNullableEncoded EncodedArray[uint64]
	benchmarkNullableValid   bool
)

func mustErrorContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want containing %q", err, want)
	}
}

func TestNullableRawFallbackPreservesValidity(t *testing.T) {
	validity, err := array.ValidityFromNulls(3, []bool{false, true, false})
	if err != nil {
		t.Fatalf("ValidityFromNulls: %v", err)
	}
	source, err := array.NewPrimitivesWithValidityUnsafe([]uint64{10, 999, 20}, validity)
	if err != nil {
		t.Fatalf("NewPrimitivesWithValidityUnsafe: %v", err)
	}

	encoded, err := UnsignedArray(source, Options{})
	if err != nil {
		t.Fatalf("UnsignedArray: %v", err)
	}
	if got := encoded.CodecType(); got != CodecTypeRaw {
		t.Fatalf("CodecType = %v, want %v", got, CodecTypeRaw)
	}
	if got := encoded.NullCount(); got != 1 {
		t.Fatalf("NullCount = %d, want 1", got)
	}
	if encoded.IsValid(1) {
		t.Fatal("IsValid(1) = true, want false")
	}

	loaded, err := LoadUnsigned[uint64](mustWriteEncodedArray(t, encoded))
	if err != nil {
		t.Fatalf("LoadUnsigned: %v", err)
	}
	if got := loaded.NullCount(); got != 1 {
		t.Fatalf("loaded NullCount = %d, want 1", got)
	}
	if loaded.IsValid(1) {
		t.Fatal("loaded IsValid(1) = true, want false")
	}
}

func TestNullableIntegerRoundTrip(t *testing.T) {
	const length = 4096
	values := make([]uint64, length)
	nulls := make([]bool, length)
	for i := range length {
		nulls[i] = true
		values[i] = uint64(i + 1000)
	}
	for _, i := range []int{5, 101, 2048, 4000} {
		nulls[i] = false
	}
	validity, err := array.ValidityFromNulls(length, nulls)
	if err != nil {
		t.Fatalf("ValidityFromNulls: %v", err)
	}
	source, err := array.NewPrimitivesWithValidityUnsafe(values, validity)
	if err != nil {
		t.Fatalf("NewPrimitivesWithValidityUnsafe: %v", err)
	}

	encoded, err := UnsignedArray(source, Options{})
	if err != nil {
		t.Fatalf("UnsignedArray: %v", err)
	}
	if got := encoded.CodecType(); got != CodecTypeNullable {
		t.Fatalf("CodecType = %v, want %v", got, CodecTypeNullable)
	}
	nullable, ok := encoded.(*nullableArray[uint64])
	if !ok {
		t.Fatalf("encoded type = %T", encoded)
	}
	if got := nullable.values.CodecType(); got != CodecTypeSparse {
		t.Fatalf("values CodecType = %v, want %v", got, CodecTypeSparse)
	}
	if got := encoded.NullCount(); got != length-4 {
		t.Fatalf("NullCount = %d, want %d", got, length-4)
	}
	for i := range uint64(length) {
		if got, want := encoded.IsValid(i), !nulls[i]; got != want {
			t.Fatalf("IsValid(%d) = %t, want %t", i, got, want)
		}
		if !nulls[i] {
			if got := encoded.ValueAt(i); got != values[i] {
				t.Fatalf("ValueAt(%d) = %d, want %d", i, got, values[i])
			}
		}
	}

	loaded, err := LoadUnsigned[uint64](mustWriteEncodedArray(t, encoded))
	if err != nil {
		t.Fatalf("LoadUnsigned: %v", err)
	}
	if got, want := loaded.NullCount(), encoded.NullCount(); got != want {
		t.Fatalf("loaded NullCount = %d, want %d", got, want)
	}
	for i := range uint64(length) {
		if got, want := loaded.IsValid(i), encoded.IsValid(i); got != want {
			t.Fatalf("loaded IsValid(%d) = %t, want %t", i, got, want)
		}
		if loaded.IsValid(i) {
			if got := loaded.ValueAt(i); got != values[i] {
				t.Fatalf("loaded ValueAt(%d) = %d, want %d", i, got, values[i])
			}
		}
	}

	sliced, err := loaded.Slice(99, 104)
	if err != nil {
		t.Fatalf("Slice: %v", err)
	}
	if got := sliced.NullCount(); got != 4 {
		t.Fatalf("sliced NullCount = %d, want 4", got)
	}
	for i, want := range []bool{false, false, true, false, false} {
		if got := sliced.IsValid(uint64(i)); got != want {
			t.Fatalf("sliced IsValid(%d) = %t, want %t", i, got, want)
		}
	}
}

func TestNullableSparseExclusionIsHonored(t *testing.T) {
	const length = 4096
	values := make([]uint64, length)
	nulls := make([]bool, length)
	for i := range nulls {
		nulls[i] = true
	}
	for _, i := range []int{5, 101, 2048, 4000} {
		nulls[i] = false
		values[i] = uint64(i)
	}
	validity, err := array.ValidityFromNulls(length, nulls)
	if err != nil {
		t.Fatalf("ValidityFromNulls: %v", err)
	}
	source, err := array.NewPrimitivesWithValidityUnsafe(values, validity)
	if err != nil {
		t.Fatalf("NewPrimitivesWithValidityUnsafe: %v", err)
	}

	encoded, err := UnsignedArray(source, Options{}.WithExcludeInteger(CodecTypeSparse))
	if err != nil {
		t.Fatalf("UnsignedArray: %v", err)
	}
	if nullable, ok := encoded.(*nullableArray[uint64]); ok {
		if got := nullable.values.CodecType(); got == CodecTypeSparse {
			t.Fatalf("values CodecType = %v, want anything else", got)
		}
	}
	for i := range source.Length() {
		if got, want := encoded.IsValid(i), source.IsValid(i); got != want {
			t.Fatalf("IsValid(%d) = %t, want %t", i, got, want)
		}
	}
}

func TestNullableAllNullUsesConstantChildren(t *testing.T) {
	const length = 4096
	validity := array.AllNull(length)
	source, err := array.NewPrimitivesWithValidityUnsafe(make([]int64, length), validity)
	if err != nil {
		t.Fatalf("NewPrimitivesWithValidityUnsafe: %v", err)
	}

	encoded, err := SignedArray(source, Options{})
	if err != nil {
		t.Fatalf("SignedArray: %v", err)
	}
	nullable, ok := encoded.(*nullableArray[int64])
	if !ok {
		t.Fatalf("encoded type = %T", encoded)
	}
	if got := nullable.values.CodecType(); got != CodecTypeConst {
		t.Fatalf("values CodecType = %v, want %v", got, CodecTypeConst)
	}
	if got := nullable.validity.CodecType(); got != CodecTypeConst {
		t.Fatalf("validity CodecType = %v, want %v", got, CodecTypeConst)
	}
	if got := nullable.NullCount(); got != length {
		t.Fatalf("NullCount = %d, want %d", got, length)
	}
}

func TestNullableSequenceIsIneligible(t *testing.T) {
	const length = 4096
	values := make([]uint64, length)
	for i := range values {
		values[i] = uint64(i)
	}
	nulls := make([]bool, length)
	nulls[0] = true // Its canonical payload remains zero, so values still form a sequence.
	validity, err := array.ValidityFromNulls(length, nulls)
	if err != nil {
		t.Fatalf("ValidityFromNulls: %v", err)
	}
	source, err := array.NewPrimitivesWithValidityUnsafe(values, validity)
	if err != nil {
		t.Fatalf("NewPrimitivesWithValidityUnsafe: %v", err)
	}

	encoded, err := UnsignedArray(source, Options{})
	if err != nil {
		t.Fatalf("UnsignedArray: %v", err)
	}
	nullable, ok := encoded.(*nullableArray[uint64])
	if !ok {
		t.Fatalf("encoded type = %T", encoded)
	}
	if got := nullable.values.CodecType(); got == CodecTypeSequence {
		t.Fatalf("values CodecType = %v, want anything else", got)
	}
}

func TestNullableStringsRoundTrip(t *testing.T) {
	const length = 2048
	values := make([]string, length)
	nulls := make([]bool, length)
	for i := range length {
		nulls[i] = true
		values[i] = "ignored"
	}
	values[17], nulls[17] = "alpha", false
	values[1024], nulls[1024] = "beta", false
	validity, err := array.ValidityFromNulls(length, nulls)
	if err != nil {
		t.Fatalf("ValidityFromNulls: %v", err)
	}
	source, err := array.NewStringsWithValidity(values, validity)
	if err != nil {
		t.Fatalf("NewStringsWithValidity: %v", err)
	}

	encoded, err := StringArray(source, Options{})
	if err != nil {
		t.Fatalf("StringArray: %v", err)
	}
	if got := encoded.CodecType(); got != CodecTypeNullable {
		t.Fatalf("CodecType = %v, want %v", got, CodecTypeNullable)
	}
	loaded, err := LoadStrings(mustWriteEncodedArray(t, encoded))
	if err != nil {
		t.Fatalf("LoadStrings: %v", err)
	}
	if got := loaded.NullCount(); got != length-2 {
		t.Fatalf("loaded NullCount = %d, want %d", got, length-2)
	}
	if got := loaded.ValueAt(17); got != "alpha" {
		t.Fatalf("ValueAt(17) = %q, want %q", got, "alpha")
	}
	if got := loaded.ValueAt(1024); got != "beta" {
		t.Fatalf("ValueAt(1024) = %q, want %q", got, "beta")
	}
	if loaded.IsValid(18) {
		t.Fatal("IsValid(18) = true, want false")
	}
}

func TestNullableStringRawChildMaterializesOnce(t *testing.T) {
	values := []string{
		strings.Repeat("discarded", 1024),
		strings.Repeat("discarded", 1024),
		"kept",
		strings.Repeat("discarded", 1024),
	}
	validity, err := array.ValidityFromNulls(4, []bool{true, true, false, true})
	if err != nil {
		t.Fatalf("ValidityFromNulls: %v", err)
	}
	source, err := array.NewStringsWithValidity(values, validity)
	if err != nil {
		t.Fatalf("NewStringsWithValidity: %v", err)
	}

	encoded, err := StringArray(source, Options{})
	if err != nil {
		t.Fatalf("StringArray: %v", err)
	}
	nullable, ok := encoded.(*nullableArray[string])
	if !ok {
		t.Fatalf("encoded type = %T", encoded)
	}
	if got := nullable.values.CodecType(); got != CodecTypeRaw {
		t.Fatalf("values CodecType = %v, want %v", got, CodecTypeRaw)
	}
	data := mustWriteEncodedArray(t, encoded)
	if got, want := uint64(len(data)), encoded.BinarySize(); got != want {
		t.Fatalf("written bytes = %d, want %d", got, want)
	}

	loaded, err := LoadStrings(data)
	if err != nil {
		t.Fatalf("LoadStrings: %v", err)
	}
	if got := loaded.ValueAt(2); got != "kept" {
		t.Fatalf("ValueAt(2) = %q, want %q", got, "kept")
	}
	if got := loaded.NullCount(); got != 3 {
		t.Fatalf("NullCount = %d, want 3", got)
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
	mustErrorContains(t, err, "const body child contains nulls")
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
		mustErrorContains(t, err, "non-zero unused bits")
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
		mustErrorContains(t, err, "bitmap has 0 nulls, header says 1")
	})
}

func BenchmarkNullableUint64(b *testing.B) {
	const length = 1 << 16
	values := make([]uint64, length)
	for i := range values {
		values[i] = uint64(i*2654435761) % 10_003
	}
	for _, ratio := range []struct {
		name   string
		isNull func(int) bool
	}{
		{name: "0", isNull: func(int) bool { return false }},
		{name: "0.01", isNull: func(i int) bool { return i%100 == 0 }},
		{name: "0.5", isNull: func(i int) bool { return i&1 == 0 }},
		{name: "0.99", isNull: func(i int) bool { return i%100 != 0 }},
		{name: "1", isNull: func(int) bool { return true }},
	} {
		nulls := make([]bool, length)
		for i := range nulls {
			nulls[i] = ratio.isNull(i)
		}
		validity, err := array.ValidityFromNulls(length, nulls)
		if err != nil {
			b.Fatal(err)
		}
		source, err := array.NewPrimitivesWithValidityUnsafe(values, validity)
		if err != nil {
			b.Fatal(err)
		}
		encoded, err := UnsignedArray(source, Options{})
		if err != nil {
			b.Fatal(err)
		}

		b.Run(fmt.Sprintf("operation=compress/nullRatio=%s", ratio.name), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				benchmarkNullableEncoded, err = UnsignedArray(source, Options{})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("operation=isValid/nullRatio=%s", ratio.name), func(b *testing.B) {
			b.ReportAllocs()
			var i uint64
			for b.Loop() {
				benchmarkNullableValid = encoded.IsValid(i & (length - 1))
				i++
			}
		})
		b.Run(fmt.Sprintf("operation=decompressInto/nullRatio=%s", ratio.name), func(b *testing.B) {
			b.ReportAllocs()
			dst := make([]uint64, length)
			b.ResetTimer()
			for b.Loop() {
				if err := encoded.DecompressInto(dst); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkNullableStringCompress(b *testing.B) {
	const length = 1 << 14
	values := make([]string, length)
	nulls := make([]bool, length)
	for i := range values {
		values[i] = fmt.Sprintf("service=%d region=%d message=request completed", i%31, i%7)
		nulls[i] = i%100 != 0
	}
	validity, err := array.ValidityFromNulls(length, nulls)
	if err != nil {
		b.Fatal(err)
	}
	source, err := array.NewStringsWithValidity(values, validity)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		encoded, err := StringArray(source, Options{})
		if err != nil {
			b.Fatal(err)
		}
		_ = encoded
	}
}
