package array

import "testing"

type uniformValiditySliceCase struct {
	source    Validity
	start     uint64
	end       uint64
	wantValid bool
}

// uniformValiditySliceCases builds globally mixed bitmaps whose selected ranges
// are uniformly valid or uniformly null at every possible starting bit offset.
func uniformValiditySliceCases(t testing.TB, length uint64) [16]uniformValiditySliceCase {
	t.Helper()

	var cases [16]uniformValiditySliceCase
	caseIndex := 0
	for start := range uint64(8) {
		for _, wantValid := range [...]bool{true, false} {
			end := start + length
			sourceLength := end + 1
			bitmap := make([]byte, validityByteLength(sourceLength))
			for i := range sourceLength {
				valid := !wantValid
				if i >= start && i < end {
					valid = wantValid
				}
				if valid {
					bitmap[i>>3] |= 1 << (i & 7)
				}
			}

			source, err := NewValidityUnsafe(sourceLength, bitmap)
			if err != nil {
				t.Fatalf("NewValidityUnsafe: %v", err)
			}
			if source.NullCount() == 0 || source.NullCount() == source.Length() {
				t.Fatal("test fixture must be globally mixed")
			}
			cases[caseIndex] = uniformValiditySliceCase{
				source:    source,
				start:     start,
				end:       end,
				wantValid: wantValid,
			}
			caseIndex++
		}
	}
	return cases
}

func assertCanonicalUniformValiditySlice(t testing.TB, got Validity, wantLength uint64, wantValid bool) {
	t.Helper()

	if got.Length() != wantLength {
		t.Fatalf("slice length = %d, want %d", got.Length(), wantLength)
	}
	wantNullCount := uint64(0)
	if !wantValid {
		wantNullCount = wantLength
	}
	if got.NullCount() != wantNullCount {
		t.Fatalf("slice null count = %d, want %d", got.NullCount(), wantNullCount)
	}
	if got.Bytes() != nil {
		t.Fatalf("canonical uniform slice retains bitmap %x", got.Bytes())
	}
	for i := range got.Length() {
		if got.IsValid(i) != wantValid {
			t.Fatalf("slice validity at %d = %t, want %t", i, got.IsValid(i), wantValid)
		}
	}
}

func TestValiditySliceCanonicalUniformRanges(t *testing.T) {
	for _, tc := range uniformValiditySliceCases(t, 17) {
		got, err := tc.source.Slice(tc.start, tc.end)
		if err != nil {
			t.Fatalf("Slice(%d, %d): %v", tc.start, tc.end, err)
		}
		assertCanonicalUniformValiditySlice(t, got, tc.end-tc.start, tc.wantValid)
	}
}

func TestStringsSliceCanonicalUniformRanges(t *testing.T) {
	for _, tc := range uniformValiditySliceCases(t, 17) {
		values := make([]string, tc.source.Length())
		for i := range values {
			values[i] = "x"
		}
		input := newStringsWithOffsets[uint16](values, uint64(len(values)), tc.source)

		slicedAny, err := input.Slice(tc.start, tc.end)
		if err != nil {
			t.Fatalf("Slice(%d, %d): %v", tc.start, tc.end, err)
		}
		sliced, ok := slicedAny.(*Strings[uint16])
		if !ok {
			t.Fatalf("Slice returned %T, want *Strings[uint16]", slicedAny)
		}
		assertCanonicalUniformValiditySlice(t, sliced.validity, tc.end-tc.start, tc.wantValid)
	}
}

func BenchmarkValiditySliceUniformMixedWidth64(b *testing.B) {
	benchmarkValiditySliceUniformMixed(b, 64)
}

func BenchmarkValiditySliceUniformMixedWidth4096(b *testing.B) {
	benchmarkValiditySliceUniformMixed(b, 4096)
}

func BenchmarkValiditySliceUniformMixedWidth65536(b *testing.B) {
	benchmarkValiditySliceUniformMixed(b, 65536)
}

func benchmarkValiditySliceUniformMixed(b *testing.B, length uint64) {
	cases := uniformValiditySliceCases(b, length)
	for _, tc := range cases {
		got, err := tc.source.Slice(tc.start, tc.end)
		if err != nil {
			b.Fatalf("Slice(%d, %d): %v", tc.start, tc.end, err)
		}
		assertCanonicalUniformValiditySlice(b, got, length, tc.wantValid)
	}

	b.ReportAllocs()
	caseIndex := 0
	var got Validity
	var err error
	for b.Loop() {
		tc := &cases[caseIndex&15]
		got, err = tc.source.Slice(tc.start, tc.end)
		if err != nil {
			b.Fatal(err)
		}
		caseIndex++
	}
	if caseIndex == 0 {
		b.Fatal("benchmark ran no iterations")
	}
	tc := cases[(caseIndex-1)&15]
	assertCanonicalUniformValiditySlice(b, got, length, tc.wantValid)
}

func BenchmarkStringsSliceUniformMixedWidth64(b *testing.B) {
	benchmarkStringsSliceUniformMixed(b, 64)
}

func BenchmarkStringsSliceUniformMixedWidth4096(b *testing.B) {
	benchmarkStringsSliceUniformMixed(b, 4096)
}

func BenchmarkStringsSliceUniformMixedWidth65536(b *testing.B) {
	benchmarkStringsSliceUniformMixed(b, 65536)
}

func benchmarkStringsSliceUniformMixed(b *testing.B, length uint64) {
	cases := uniformValiditySliceCases(b, length)
	var inputs [16]*Strings[uint32]
	for i, tc := range cases {
		values := make([]string, tc.source.Length())
		for j := range values {
			values[j] = "value"
		}
		inputs[i] = newStringsWithOffsets[uint32](values, uint64(len(values)*len("value")), tc.source)

		slicedAny, err := inputs[i].Slice(tc.start, tc.end)
		if err != nil {
			b.Fatalf("Slice(%d, %d): %v", tc.start, tc.end, err)
		}
		sliced, ok := slicedAny.(*Strings[uint32])
		if !ok {
			b.Fatalf("Slice returned %T, want *Strings[uint32]", slicedAny)
		}
		assertCanonicalUniformValiditySlice(b, sliced.validity, length, tc.wantValid)
	}

	b.ReportAllocs()
	caseIndex := 0
	var got Array[string]
	var err error
	for b.Loop() {
		tc := &cases[caseIndex&15]
		got, err = inputs[caseIndex&15].Slice(tc.start, tc.end)
		if err != nil {
			b.Fatal(err)
		}
		caseIndex++
	}
	if caseIndex == 0 {
		b.Fatal("benchmark ran no iterations")
	}
	tc := cases[(caseIndex-1)&15]
	sliced, ok := got.(*Strings[uint32])
	if !ok {
		b.Fatalf("Slice returned %T, want *Strings[uint32]", got)
	}
	assertCanonicalUniformValiditySlice(b, sliced.validity, length, tc.wantValid)
}
