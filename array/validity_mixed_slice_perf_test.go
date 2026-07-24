package array

import "testing"

type mixedFirstValidStringsSliceCase struct {
	input         *Strings[uint32]
	start         uint64
	end           uint64
	transition    uint64
	wantNullCount uint64
}

// mixedFirstValidStringsSliceCases builds globally mixed inputs whose selected
// ranges start valid, cross a complete rebased byte before their first null,
// and retain a mixed suffix.
func mixedFirstValidStringsSliceCases(t testing.TB, length uint64) [4]mixedFirstValidStringsSliceCase {
	t.Helper()

	transitions := [...]uint64{8, 9, 16, length / 2}
	starts := [...]uint64{3, 5, 7, 1}
	var cases [4]mixedFirstValidStringsSliceCase
	for caseIndex, transition := range transitions {
		start := starts[caseIndex]
		end := start + length
		sourceLength := end + 1
		bitmap := make([]byte, validityByteLength(sourceLength))
		wantNullCount := uint64(0)
		for i := range sourceLength {
			valid := false
			if i >= start && i < end {
				offset := i - start
				valid = offset < transition || (offset > transition && offset&1 == 0)
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
			t.Fatal("benchmark fixture must be globally mixed")
		}

		values := make([]string, sourceLength)
		for i := range values {
			values[i] = "value"
		}
		cases[caseIndex] = mixedFirstValidStringsSliceCase{
			input:         newStringsWithOffsets[uint32](values, uint64(len(values)*len("value")), validity),
			start:         start,
			end:           end,
			transition:    transition,
			wantNullCount: wantNullCount,
		}
	}
	return cases
}

func (tc mixedFirstValidStringsSliceCase) validAt(offset uint64) bool {
	return offset < tc.transition || (offset > tc.transition && offset&1 == 0)
}

func assertMixedFirstValidStringsSlice(t testing.TB, got *Strings[uint32], tc mixedFirstValidStringsSliceCase) {
	t.Helper()

	length := tc.end - tc.start
	if got.Length() != length {
		t.Fatalf("slice length = %d, want %d", got.Length(), length)
	}
	if got.validity.Bytes() == nil {
		t.Fatal("mixed slice discarded validity bitmap")
	}
	if got.NullCount() != tc.wantNullCount {
		t.Fatalf("slice null count = %d, want %d", got.NullCount(), tc.wantNullCount)
	}
	for i := range length {
		if got.IsValid(i) != tc.validAt(i) {
			t.Fatalf("slice validity at %d = %t, want %t", i, got.IsValid(i), tc.validAt(i))
		}
	}
}

func BenchmarkStringsSliceMixedFirstValidWidth4096(b *testing.B) {
	const length uint64 = 4096
	cases := mixedFirstValidStringsSliceCases(b, length)
	for _, tc := range cases {
		slicedAny, err := tc.input.Slice(tc.start, tc.end)
		if err != nil {
			b.Fatalf("Slice(%d, %d): %v", tc.start, tc.end, err)
		}
		sliced, ok := slicedAny.(*Strings[uint32])
		if !ok {
			b.Fatalf("Slice returned %T, want *Strings[uint32]", slicedAny)
		}
		assertMixedFirstValidStringsSlice(b, sliced, tc)
	}

	b.ReportAllocs()
	caseIndex := 0
	var got Array[string]
	var err error
	for b.Loop() {
		tc := &cases[caseIndex&3]
		got, err = tc.input.Slice(tc.start, tc.end)
		if err != nil {
			b.Fatal(err)
		}
		caseIndex++
	}
	if caseIndex == 0 {
		b.Fatal("benchmark ran no iterations")
	}
	last := cases[(caseIndex-1)&3]
	sliced, ok := got.(*Strings[uint32])
	if !ok {
		b.Fatalf("Slice returned %T, want *Strings[uint32]", got)
	}
	assertMixedFirstValidStringsSlice(b, sliced, last)
}
