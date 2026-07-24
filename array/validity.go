package array

import (
	"errors"
	"fmt"
	"io"
	"math/bits"
)

// Validity is an immutable bitmap where a set bit marks a valid value. A
// zero-value Validity represents an empty, all-valid array; use AllValid when
// the length is non-zero.
//
// Keep memory passed to NewValidityUnsafe alive and unchanged for the
// lifetime of the Validity.
type Validity struct {
	bits      []byte
	length    uint64
	nullCount uint64
}

// ErrValidityBitmapLimit reports that ValidityBitmap would exceed its
// configured allocation limit.
var ErrValidityBitmapLimit = errors.New("array: validity bitmap allocation limit exceeded")

// ValiditySource is the minimum an array must expose for ValidityBitmap to
// derive a bitmap from it: a row count and per-row validity.
type ValiditySource interface {
	// Length is the number of rows the source describes.
	Length() uint64
	// IsValid reports whether the row at offset is non-null. Panics if
	// offset >= Length().
	IsValid(offset uint64) bool
}

// AllValid returns validity metadata for an array without null values.
func AllValid(length uint64) Validity {
	return Validity{length: length}
}

// AllNull returns validity metadata for an array whose values are all null.
func AllNull(length uint64) Validity {
	if length == 0 {
		return Validity{}
	}
	return Validity{
		length:    length,
		nullCount: length,
	}
}

// NewValidity builds validity metadata from a copy of bits. Bit i is one when
// value i is valid. bits must contain exactly ceil(length/8) bytes and unused
// high bits in the last byte must be zero.
func NewValidity(length uint64, bitmap []byte) (Validity, error) {
	cloned := make([]byte, len(bitmap))
	copy(cloned, bitmap)
	return NewValidityUnsafe(length, cloned)
}

// NewValidityUnsafe builds validity metadata backed by bitmap. The caller must
// not modify bitmap after construction.
func NewValidityUnsafe(length uint64, bitmap []byte) (Validity, error) {
	want := validityByteLength(length)
	if want > platformSliceLimit() {
		return Validity{}, errors.New("array: validity bitmap exceeds platform limit")
	}
	if uint64(len(bitmap)) != want {
		return Validity{}, fmt.Errorf("array: validity bitmap length = %d, want %d", len(bitmap), want)
	}
	if length == 0 {
		return Validity{}, nil
	}
	if remainder := length & 7; remainder != 0 {
		unused := ^byte((1 << remainder) - 1)
		if bitmap[len(bitmap)-1]&unused != 0 {
			return Validity{}, errors.New("array: validity bitmap has non-zero unused bits")
		}
	}
	validCount := uint64(0)
	for _, b := range bitmap {
		validCount += uint64(bits.OnesCount8(b))
	}
	if validCount == length {
		return AllValid(length), nil
	}
	if validCount == 0 {
		return AllNull(length), nil
	}
	return Validity{
		bits:      bitmap,
		length:    length,
		nullCount: length - validCount,
	}, nil
}

// ValidityFromNulls converts a bool mask using true=null semantics. An empty
// mask represents an all-valid array of length.
func ValidityFromNulls(length uint64, nulls []bool) (Validity, error) {
	if len(nulls) == 0 {
		return AllValid(length), nil
	}
	if uint64(len(nulls)) != length {
		return Validity{}, fmt.Errorf("array: null mask length = %d, want %d", len(nulls), length)
	}
	bitmap := make([]byte, validityByteLength(length))
	for i, isNull := range nulls {
		if !isNull {
			bitmap[i>>3] |= byte(1 << (uint(i) & 7))
		}
	}
	return NewValidityUnsafe(length, bitmap)
}

// ValidityBitmap returns the wire bitmap and null count for a source range.
// The returned bytes are always read-only and may alias source-owned storage;
// callers must not modify them. A whole-array request may borrow an available
// bitmap, while other requests materialize a rebased bitmap.
func ValidityBitmap(source ValiditySource, start, length uint64, opts ...BuildOptions) ([]byte, uint64, error) {
	if source == nil {
		return nil, 0, errors.New("array: nil validity source")
	}
	// Ahead of the borrow fast path, so the same arguments are accepted or
	// rejected whether or not the source can hand over its bitmap.
	maxBytes, err := buildByteLimit(opts)
	if err != nil {
		return nil, 0, err
	}
	sourceLength := source.Length()
	if start > sourceLength || length > sourceLength-start {
		return nil, 0, fmt.Errorf("array: validity range start=%d length=%d outside source length %d", start, length, sourceLength)
	}
	byteLength := validityByteLength(length)
	if byteLength > platformSliceLimit() {
		return nil, 0, errors.New("array: validity bitmap exceeds platform limit")
	}
	if start == 0 && length == sourceLength {
		if provider, ok := source.(interface{ Validity() Validity }); ok {
			validity := provider.Validity()
			if validity.Length() == length && validity.NullCount() <= length && uint64(len(validity.Bytes())) == byteLength && validity.Bytes() != nil {
				return validity.Bytes(), validity.NullCount(), nil
			}
		}
	}
	if byteLength > maxBytes {
		return nil, 0, fmt.Errorf("%w: requires %d bytes, limit %d", ErrValidityBitmapLimit, byteLength, maxBytes)
	}
	bitmap := make([]byte, byteLength)
	validCount := uint64(0)
	for i := range length {
		if source.IsValid(start + i) {
			bitmap[i>>3] |= 1 << (i & 7)
			validCount++
		}
	}
	return bitmap, length - validCount, nil
}

// Length returns the number of rows described by v.
func (v Validity) Length() uint64 { return v.length }

// NullCount returns the number of invalid rows.
func (v Validity) NullCount() uint64 { return v.nullCount }

// Bytes returns v's wire bitmap, or nil when v carries no bitmap (all-valid or
// all-null). The caller must not modify it.
func (v Validity) Bytes() []byte { return v.bits }

// BinarySize returns the number of bytes in v's wire bitmap. All-valid
// validity has no wire payload.
func (v Validity) BinarySize() uint64 {
	if v.nullCount == 0 {
		return 0
	}
	return validityByteLength(v.length)
}

// IsValid reports whether row i is valid. It panics when i is out of bounds.
func (v Validity) IsValid(i uint64) bool {
	if i >= v.length {
		panic("array: validity index out of range")
	}
	if v.nullCount == 0 {
		return true
	}
	if v.bits == nil {
		return false
	}
	return v.bits[i>>3]&(1<<(i&7)) != 0
}

// Slice returns validity for [start, end), materializing a rebased bitmap
// only when the range contains both valid and null rows.
func (v Validity) Slice(start, end uint64) (Validity, error) {
	if err := ValidateSliceBounds(v.length, start, end); err != nil {
		return Validity{}, err
	}
	length := end - start
	if length == 0 || v.nullCount == 0 {
		return AllValid(length), nil
	}
	if v.nullCount == v.length {
		return AllNull(length), nil
	}

	firstValid := v.IsValid(start)
	for i := uint64(1); i < length; i++ {
		if v.IsValid(start+i) == firstValid {
			continue
		}

		bitmap := make([]byte, validityByteLength(length))
		nullCount := i
		if firstValid {
			nullCount = 1
			fullBytes := i / 8
			for j := range fullBytes {
				bitmap[j] = 0xff
			}
			if remainder := i & 7; remainder != 0 {
				bitmap[fullBytes] = byte((1 << remainder) - 1)
			}
		} else {
			bitmap[i>>3] |= byte(1 << (i & 7))
		}
		for i++; i < length; i++ {
			if v.IsValid(start + i) {
				bitmap[i>>3] |= byte(1 << (i & 7))
			} else {
				nullCount++
			}
		}
		return Validity{
			bits:      bitmap,
			length:    length,
			nullCount: nullCount,
		}, nil
	}
	if firstValid {
		return AllValid(length), nil
	}
	return AllNull(length), nil
}

func validityByteLength(length uint64) uint64 {
	byteLength := length / 8
	if length&7 != 0 {
		byteLength++
	}
	return byteLength
}

// WriteTo writes v's bitmap without a header.
func (v Validity) WriteTo(w io.Writer) (int64, error) {
	if v.nullCount == 0 {
		return 0, nil
	}
	if v.bits == nil {
		return writeZeroBytes(w, validityByteLength(v.length))
	}
	n, err := w.Write(v.bits)
	if err == nil && n != len(v.bits) {
		err = io.ErrShortWrite
	}
	return int64(n), err
}

func writeZeroBytes(w io.Writer, length uint64) (int64, error) {
	const bufferBytes = 4096
	var zeros [bufferBytes]byte
	var written int64
	for length > 0 {
		chunk := min(length, uint64(len(zeros)))
		n, err := w.Write(zeros[:chunk])
		written += int64(n)
		if err != nil {
			return written, err
		}
		if uint64(n) != chunk {
			return written, io.ErrShortWrite
		}
		length -= chunk
	}
	return written, nil
}

func readValidityFromBuf(br *BufReader, length uint64) (Validity, error) {
	byteLength := validityByteLength(length)
	if byteLength > platformSliceLimit() {
		return Validity{}, errors.New("array: validity bitmap exceeds platform limit")
	}
	bitmap, err := br.Read(int(byteLength))
	if err != nil {
		return Validity{}, fmt.Errorf("array: reading validity bitmap: %w", err)
	}
	validity, err := NewValidityUnsafe(length, bitmap)
	if err != nil {
		return Validity{}, err
	}
	if validity.NullCount() == 0 {
		return Validity{}, errors.New("array: validity flag set for all-valid bitmap")
	}
	return validity, nil
}
