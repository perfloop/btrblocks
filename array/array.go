package array

import (
	"errors"
	"fmt"
	"io"
)

// ArrayCore is the minimal read interface for columnar data: values, validity,
// and length. Used by planner estimation and codec build paths that only need
// to scan an array.
type ArrayCore[T Integer | Float | String] interface {
	// ValueAt returns the physical value at the given index. Callers must consult
	// IsValid before interpreting it. Panics if offset >= Length().
	ValueAt(offset uint64) T
	// Length is the number of elements in the array.
	Length() uint64
	// IsValid reports whether the value at offset is non-null. Panics if
	// offset >= Length().
	IsValid(offset uint64) bool
	// NullCount returns the number of null values.
	NullCount() uint64
}

// Array is a fully materialized columnar array that can be serialized and sliced.
type Array[T Integer | Float | String] interface {
	ArrayCore[T]
	io.WriterTo
	// CopyTo copies all elements into dst. len(dst) must be >= Length().
	CopyTo(dst []T)
	// Slice returns a view of the half-open interval [start, end).
	Slice(start, end uint64) (Array[T], error)
	// BinarySize is the total size in bytes when written (header + body).
	BinarySize() uint64
	// PType identifies the element type for this array.
	PType() PType
}

// ValidateSliceBounds validates the half-open range [start, end) against length.
func ValidateSliceBounds(length, start, end uint64) error {
	if start > end {
		return fmt.Errorf("array: slice start = %d, want <= %d", start, end)
	}
	if end > length {
		return fmt.Errorf("array: slice end = %d, want <= %d", end, length)
	}
	return nil
}

// MaterializePrimitiveSlice copies [start, end) into owned primitive storage.
func MaterializePrimitiveSlice[T PrimitiveType](src ArrayCore[T], start, end uint64) (*Primitives[T], error) {
	if err := ValidateSliceBounds(src.Length(), start, end); err != nil {
		return nil, err
	}
	values := make([]T, end-start)
	for i := range values {
		values[i] = src.ValueAt(start + uint64(i))
	}
	validity, err := materializeValiditySlice(src, start, end)
	if err != nil {
		return nil, err
	}
	return NewPrimitivesWithValidityUnsafe(values, validity)
}

// MaterializeStringSlice copies [start, end) into owned string storage.
func MaterializeStringSlice(src ArrayCore[string], start, end uint64) (Array[string], error) {
	if err := ValidateSliceBounds(src.Length(), start, end); err != nil {
		return nil, err
	}
	values := make([]string, end-start)
	for i := range values {
		values[i] = src.ValueAt(start + uint64(i))
	}
	validity, err := materializeValiditySlice(src, start, end)
	if err != nil {
		return nil, err
	}
	return NewStringsWithValidity(values, validity)
}

func materializeValiditySlice(src interface {
	Length() uint64
	IsValid(uint64) bool
	NullCount() uint64
}, start, end uint64) (Validity, error) {
	length := end - start
	if src.NullCount() == 0 {
		return AllValid(length), nil
	}
	bitmap := make([]byte, validityByteLength(length))
	for i := range length {
		if src.IsValid(start + i) {
			bitmap[i>>3] |= byte(1 << (i & 7))
		}
	}
	return NewValidityUnsafe(length, bitmap)
}

// readOpts collapses a variadic ReadOptions tail to the single option the
// readers honour. Extras are rejected rather than silently dropped: accepting
// them now would make it a breaking change to give them meaning later.
func readOpts(opts []ReadOptions) (ReadOptions, error) {
	if len(opts) > 1 {
		return ReadOptions{}, fmt.Errorf("array: at most one ReadOptions is allowed, got %d", len(opts))
	}
	if len(opts) == 1 {
		return opts[0], nil
	}
	return ReadOptions{}, nil
}

func readArrayHeader(br *BufReader, opts []ReadOptions) (Header, error) {
	if br == nil {
		return Header{}, errors.New("array: nil buffer reader")
	}
	option, err := readOpts(opts)
	if err != nil {
		return Header{}, err
	}
	header, err := readHeaderFromBuf(br)
	if err != nil {
		return Header{}, fmt.Errorf("array: reading header: %w", err)
	}
	if err := validateHeader(header, option); err != nil {
		return Header{}, err
	}
	return header, nil
}

// ReadPrimitiveFromBuf decodes one zero-copy numeric array and advances br.
// Keep br.Buf alive and unchanged for the lifetime of the result.
func ReadPrimitiveFromBuf[T PrimitiveType](br *BufReader, opts ...ReadOptions) (*Primitives[T], error) {
	header, err := readArrayHeader(br, opts)
	if err != nil {
		return nil, err
	}
	primitive, err := readPrimitivesFromBuf[T](br, header)
	if err != nil {
		return nil, fmt.Errorf("array: reading primitive body: %w", err)
	}
	return primitive, nil
}

// ReadStringsFromBuf decodes one zero-copy string array and advances br. Keep
// br.Buf alive and unchanged for the lifetime of the result.
func ReadStringsFromBuf(br *BufReader, opts ...ReadOptions) (Array[string], error) {
	header, err := readArrayHeader(br, opts)
	if err != nil {
		return nil, err
	}
	strings, err := readStringsFromBuf(br, header)
	if err != nil {
		return nil, fmt.Errorf("array: reading string body: %w", err)
	}
	return strings, nil
}
