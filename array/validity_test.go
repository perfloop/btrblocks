package array

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

type validityProvider struct {
	length   uint64
	validity Validity
}

func (s validityProvider) Length() uint64      { return s.length }
func (s validityProvider) IsValid(uint64) bool { return true }
func (s validityProvider) Validity() Validity  { return s.validity }

func TestValidityFromNullsAndSlice(t *testing.T) {
	validity, err := ValidityFromNulls(10, []bool{false, true, false, false, true, false, false, true, false, false})
	if err != nil {
		t.Fatalf("ValidityFromNulls: %v", err)
	}
	if got := validity.NullCount(); got != 3 {
		t.Fatalf("NullCount = %d, want 3", got)
	}
	for i, want := range []bool{true, false, true, true, false, true, true, false, true, true} {
		if got := validity.IsValid(uint64(i)); got != want {
			t.Fatalf("IsValid(%d) = %t, want %t", i, got, want)
		}
	}

	sliced, err := validity.Slice(1, 9)
	if err != nil {
		t.Fatalf("Slice: %v", err)
	}
	if got := sliced.NullCount(); got != 3 {
		t.Fatalf("sliced NullCount = %d, want 3", got)
	}
	for i, want := range []bool{false, true, true, false, true, true, false, true} {
		if got := sliced.IsValid(uint64(i)); got != want {
			t.Fatalf("sliced IsValid(%d) = %t, want %t", i, got, want)
		}
	}
}

func TestValidityFromEmptyNullsMeansAllValid(t *testing.T) {
	validity, err := ValidityFromNulls(3, []bool{})
	if err != nil {
		t.Fatalf("ValidityFromNulls: %v", err)
	}
	if got := validity.NullCount(); got != 0 {
		t.Fatalf("NullCount = %d, want 0", got)
	}
	for i := range validity.Length() {
		if !validity.IsValid(i) {
			t.Fatalf("IsValid(%d) = false, want true", i)
		}
	}
}

func TestValidityBitmapRejectsInvalidRange(t *testing.T) {
	validity := AllValid(4)
	if _, _, err := ValidityBitmap(validity, 3, 2); err == nil {
		t.Fatal("ValidityBitmap accepted an out-of-range request")
	}
}

func TestValidityBitmapHonorsAllocationBudget(t *testing.T) {
	source := AllValid(16)
	if _, _, err := ValidityBitmap(source, 0, 16, BuildOptions{MaxBytes: 1}); !errors.Is(err, ErrValidityBitmapLimit) {
		t.Fatalf("small-budget error = %v, want %v", err, ErrValidityBitmapLimit)
	}
	bitmap, nullCount, err := ValidityBitmap(source, 0, 16, BuildOptions{MaxBytes: 2})
	if err != nil {
		t.Fatalf("sufficient-budget error = %v", err)
	}
	if len(bitmap) != 2 || nullCount != 0 {
		t.Fatalf("bitmap length = %d, null count = %d; want 2, 0", len(bitmap), nullCount)
	}

	hostile := AllValid((DefaultMaxBuildBytes + 1) * 8)
	if _, _, err := ValidityBitmap(hostile, 0, hostile.Length()); !errors.Is(err, ErrValidityBitmapLimit) {
		t.Fatalf("default-budget error = %v, want %v", err, ErrValidityBitmapLimit)
	}
}

// TestValidityBitmapRejectsExtraOptions pins the pre-v1 decision that a
// variadic options tail with more than one element is an error, on the borrow
// fast path as well as the materializing one. Silently honouring opts[0] can
// never start rejecting once the surface is tagged.
func TestValidityBitmapRejectsExtraOptions(t *testing.T) {
	borrowable, err := NewValidityUnsafe(16, []byte{0x0f, 0x0f})
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string]ValiditySource{
		"materialized": AllValid(16),
		"borrowed":     validityProvider{length: 16, validity: borrowable},
	}
	for name, source := range sources {
		t.Run(name, func(t *testing.T) {
			_, _, err := ValidityBitmap(source, 0, 16, BuildOptions{MaxBytes: 2}, BuildOptions{MaxBytes: 4})
			if err == nil || !strings.Contains(err.Error(), "at most one BuildOptions") {
				t.Fatalf("ValidityBitmap error = %v, want at-most-one rejection", err)
			}
		})
	}
}

func TestValidityBitmapRejectsMismatchedBorrowedBitmap(t *testing.T) {
	validity, err := NewValidityUnsafe(8, []byte{1})
	if err != nil {
		t.Fatal(err)
	}
	source := validityProvider{length: 16, validity: validity}
	bitmap, nullCount, err := ValidityBitmap(source, 0, source.Length())
	if err != nil {
		t.Fatal(err)
	}
	if len(bitmap) != 2 || bitmap[0] != 0xff || bitmap[1] != 0xff || nullCount != 0 {
		t.Fatalf("bitmap = %x, null count = %d; want ffff, 0", bitmap, nullCount)
	}
}

func TestValiditySliceRebasesBitmap(t *testing.T) {
	bitmap := []byte{0x81, 0x42, 0x24, 0x18, 0x18, 0x24, 0x42, 0x81, 0x03}
	validity, err := NewValidityUnsafe(66, bitmap)
	if err != nil {
		t.Fatalf("NewValidityUnsafe: %v", err)
	}

	sliced, err := validity.Slice(3, 66)
	if err != nil {
		t.Fatalf("Slice: %v", err)
	}
	var buf bytes.Buffer
	if _, err := sliced.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	rebased := buf.Bytes()
	if got, want := uint64(len(rebased)), sliced.BinarySize(); got != want {
		t.Fatalf("written bytes = %d, want %d", got, want)
	}
	if !bytes.Equal(rebased, sliced.Bytes()) {
		t.Fatalf("written bitmap = %x, Bytes() = %x", rebased, sliced.Bytes())
	}
	for i := range sliced.Length() {
		if got, want := rebased[i>>3]&(1<<(i&7)) != 0, validity.IsValid(3+i); got != want {
			t.Fatalf("rebased bit %d = %t, want %t", i, got, want)
		}
	}
	if remainder := sliced.Length() & 7; remainder != 0 {
		if unused := rebased[len(rebased)-1] &^ byte(1<<remainder-1); unused != 0 {
			t.Fatalf("rebased bitmap has non-zero unused bits %#x", unused)
		}
	}
}

func TestValiditySliceRebasesFirstValidFullBytePrefix(t *testing.T) {
	const (
		start       uint64 = 3
		sliceLength uint64 = 19
	)
	for _, transition := range [...]uint64{8, 9, 16} {
		end := start + sliceLength
		sourceLength := end + 1
		bitmap := make([]byte, validityByteLength(sourceLength))
		want := make([]bool, sliceLength)
		wantNullCount := uint64(0)
		for i := range sourceLength {
			valid := false
			if i >= start && i < end {
				offset := i - start
				switch {
				case offset < transition:
					valid = true
				case offset > transition:
					valid = offset&1 == 0
				}
				want[offset] = valid
				if !valid {
					wantNullCount++
				}
			}
			if valid {
				bitmap[i>>3] |= 1 << (i & 7)
			}
		}
		validity, err := NewValidityUnsafe(sourceLength, bitmap)
		if err != nil {
			t.Fatalf("NewValidityUnsafe: %v", err)
		}
		if validity.NullCount() == 0 || validity.NullCount() == validity.Length() {
			t.Fatal("test fixture must be globally mixed")
		}

		sliced, err := validity.Slice(start, end)
		if err != nil {
			t.Fatalf("Slice(%d, %d): %v", start, end, err)
		}
		if sliced.Bytes() == nil {
			t.Fatalf("Slice(%d, %d) discarded mixed bitmap", start, end)
		}
		if got := sliced.NullCount(); got != wantNullCount {
			t.Fatalf("Slice(%d, %d) null count = %d, want %d", start, end, got, wantNullCount)
		}
		for i, wantValid := range want {
			if got := sliced.IsValid(uint64(i)); got != wantValid {
				t.Fatalf("Slice(%d, %d) validity at %d = %t, want %t", start, end, i, got, wantValid)
			}
			if got := sliced.Bytes()[i>>3]&(1<<(uint(i)&7)) != 0; got != wantValid {
				t.Fatalf("Slice(%d, %d) bitmap bit %d = %t, want %t", start, end, i, got, wantValid)
			}
		}
	}
}

func TestNewValidityRejectsInvalidBitmap(t *testing.T) {
	if _, err := NewValidity(9, []byte{0xff}); err == nil || !strings.Contains(err.Error(), "length") {
		t.Fatalf("error = %v, want containing %q", err, "length")
	}

	if _, err := NewValidity(9, []byte{0xff, 0x80}); err == nil || !strings.Contains(err.Error(), "unused bits") {
		t.Fatalf("error = %v, want containing %q", err, "unused bits")
	}
}

func TestNullablePrimitivesRoundTrip(t *testing.T) {
	validity, err := ValidityFromNulls(5, []bool{false, true, false, true, false})
	if err != nil {
		t.Fatalf("ValidityFromNulls: %v", err)
	}
	arr, err := NewPrimitivesWithValidity([]uint32{10, 99, 20, 88, 30}, validity)
	if err != nil {
		t.Fatalf("NewPrimitivesWithValidity: %v", err)
	}
	if got := arr.NullCount(); got != 2 {
		t.Fatalf("NullCount = %d, want 2", got)
	}
	if arr.IsValid(1) {
		t.Fatal("IsValid(1) = true, want false")
	}
	if got, want := arr.BinarySize(), uint64(HeaderSize+1+5*4); got != want {
		t.Fatalf("BinarySize = %d, want %d", got, want)
	}

	var buf bytes.Buffer
	if _, err = arr.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if got := binaryHeaderFlags(buf.Bytes()); got != FlagValidity {
		t.Fatalf("header flags = %#x, want %#x", got, FlagValidity)
	}

	got, err := readPrimitiveFromBytes[uint32](buf.Bytes())
	if err != nil {
		t.Fatalf("readPrimitiveFromBytes: %v", err)
	}
	if got.NullCount() != arr.NullCount() {
		t.Fatalf("loaded NullCount = %d, want %d", got.NullCount(), arr.NullCount())
	}
	for i := range arr.Length() {
		if got.IsValid(i) != arr.IsValid(i) {
			t.Fatalf("row %d validity = %t, want %t", i, got.IsValid(i), arr.IsValid(i))
		}
		if got.ValueAt(i) != arr.ValueAt(i) {
			t.Fatalf("row %d value = %d, want %d", i, got.ValueAt(i), arr.ValueAt(i))
		}
	}

	sliced, err := got.Slice(1, 4)
	if err != nil {
		t.Fatalf("Slice: %v", err)
	}
	if got := sliced.NullCount(); got != 2 {
		t.Fatalf("sliced NullCount = %d, want 2", got)
	}
	for i, want := range []bool{false, true, false} {
		if got := sliced.IsValid(uint64(i)); got != want {
			t.Fatalf("sliced IsValid(%d) = %t, want %t", i, got, want)
		}
	}
}

func TestNullableStringsRoundTrip(t *testing.T) {
	validity, err := ValidityFromNulls(4, []bool{true, false, true, false})
	if err != nil {
		t.Fatalf("ValidityFromNulls: %v", err)
	}
	arr, err := NewStringsWithValidity([]string{"ignored", "go", "ignored", "lang"}, validity)
	if err != nil {
		t.Fatalf("NewStringsWithValidity: %v", err)
	}

	var buf bytes.Buffer
	if _, err = arr.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	got, err := readStringsFromBytes(buf.Bytes())
	if err != nil {
		t.Fatalf("readStringsFromBytes: %v", err)
	}
	if got.NullCount() != 2 {
		t.Fatalf("loaded NullCount = %d, want 2", got.NullCount())
	}
	for i := range arr.Length() {
		if got.IsValid(i) != arr.IsValid(i) {
			t.Fatalf("row %d validity = %t, want %t", i, got.IsValid(i), arr.IsValid(i))
		}
		if got.ValueAt(i) != arr.ValueAt(i) {
			t.Fatalf("row %d value = %q, want %q", i, got.ValueAt(i), arr.ValueAt(i))
		}
	}
}

func binaryHeaderFlags(data []byte) uint16 {
	return uint16(data[2]) | uint16(data[3])<<8
}
