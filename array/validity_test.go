package array

import (
	"bytes"
	"strings"
	"testing"
)

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
