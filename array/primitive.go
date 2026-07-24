//go:build 386 || amd64 || arm || arm64 || loong64 || mips64le || mipsle || ppc64le || riscv64 || wasm

package array

import (
	"errors"
	"fmt"
	"io"
	"unsafe"
)

var (
	_ Array[int8]    = (*Primitives[int8])(nil)
	_ Array[int16]   = (*Primitives[int16])(nil)
	_ Array[int32]   = (*Primitives[int32])(nil)
	_ Array[int64]   = (*Primitives[int64])(nil)
	_ Array[uint8]   = (*Primitives[uint8])(nil)
	_ Array[uint16]  = (*Primitives[uint16])(nil)
	_ Array[uint32]  = (*Primitives[uint32])(nil)
	_ Array[uint64]  = (*Primitives[uint64])(nil)
	_ Array[float32] = (*Primitives[float32])(nil)
	_ Array[float64] = (*Primitives[float64])(nil)
)

// Primitives is a columnar array of fixed-width primitive values (int/uint/float).
// ValueAt is O(1); the underlying slice is stored directly.
type Primitives[T PrimitiveType] struct {
	pType    PType
	data     []T
	validity Validity
}

// NewPrimitives builds a Primitives array from a copy of values. The returned array does not share storage with the input.
func NewPrimitives[T PrimitiveType](values []T) *Primitives[T] {
	data := make([]T, len(values))
	copy(data, values)
	return NewPrimitivesUnsafe(data)
}

// NewPrimitivesWithNulls builds a primitive array from a copy of values. A
// true entry in nulls marks the corresponding value as null. An empty nulls
// slice means all values are valid; otherwise its length must equal
// len(values).
func NewPrimitivesWithNulls[T PrimitiveType](values []T, nulls []bool) (*Primitives[T], error) {
	validity, err := ValidityFromNulls(uint64(len(values)), nulls)
	if err != nil {
		return nil, fmt.Errorf("array: primitive nulls: %w", err)
	}
	return NewPrimitivesWithValidity(values, validity)
}

// NewPrimitivesUnsafe builds a Primitives array that uses the given slice as its backing storage. The caller must not modify the slice after construction.
func NewPrimitivesUnsafe[T PrimitiveType](data []T) *Primitives[T] {
	pType := PTypeOfPrimitive[T]()
	return &Primitives[T]{pType: pType, data: data, validity: AllValid(uint64(len(data)))}
}

// NewPrimitivesWithValidity builds a primitive array from a copy of values and
// immutable validity metadata.
func NewPrimitivesWithValidity[T PrimitiveType](values []T, validity Validity) (*Primitives[T], error) {
	data := make([]T, len(values))
	copy(data, values)
	return NewPrimitivesWithValidityUnsafe(data, validity)
}

// NewPrimitivesWithValidityUnsafe builds a primitive array backed by values.
// The caller must not modify values after construction.
func NewPrimitivesWithValidityUnsafe[T PrimitiveType](values []T, validity Validity) (*Primitives[T], error) {
	if validity.Length() != uint64(len(values)) {
		return nil, fmt.Errorf("array: validity length = %d, want %d", validity.Length(), len(values))
	}
	return &Primitives[T]{
		pType:    PTypeOfPrimitive[T](),
		data:     values,
		validity: validity,
	}, nil
}

func (c *Primitives[T]) ValueAt(offset uint64) T { return c.data[offset] }
func (c *Primitives[T]) CopyTo(dst []T)          { copy(dst, c.data) }
func (c *Primitives[T]) IsValid(offset uint64) bool {
	return c.validity.IsValid(offset)
}

// Validity returns the immutable validity metadata for c.
func (c *Primitives[T]) Validity() Validity { return c.validity }
func (c *Primitives[T]) NullCount() uint64  { return c.validity.NullCount() }

// ValuesUnsafe returns the backing values without copying. Callers must not
// modify the returned slice.
func (c *Primitives[T]) ValuesUnsafe() []T { return c.data }
func (c *Primitives[T]) BinarySize() uint64 {
	return uint64(headerSize) + c.validity.BinarySize() + c.bodySize()
}
func (c *Primitives[T]) Length() uint64   { return uint64(len(c.data)) }
func (c *Primitives[T]) PType() PType     { return c.pType }
func (c *Primitives[T]) bodySize() uint64 { return uint64(len(c.data)) * uint64(c.width()) }

func (c *Primitives[T]) Slice(start, end uint64) (Array[T], error) {
	if err := ValidateSliceBounds(c.Length(), start, end); err != nil {
		return nil, err
	}
	validity, err := c.validity.Slice(start, end)
	if err != nil {
		return nil, err
	}
	return &Primitives[T]{pType: c.pType, data: c.data[int(start):int(end)], validity: validity}, nil
}

func (c *Primitives[T]) writeBody(w io.Writer) (int64, error) {
	if len(c.data) == 0 {
		return 0, nil
	}
	width := c.width()
	byteLen := len(c.data) * width
	b := unsafe.Slice((*byte)(unsafe.Pointer(&c.data[0])), byteLen)
	n, err := w.Write(b)
	if err == nil && n != len(b) {
		err = io.ErrShortWrite
	}
	return int64(n), err
}

func (c *Primitives[T]) WriteTo(w io.Writer) (int64, error) {
	flags := uint16(0)
	if c.NullCount() != 0 {
		flags |= FlagValidity
	}
	hn, err := Header{
		Version:  FormatVersion,
		PType:    c.pType,
		Flags:    flags,
		Length:   c.Length(),
		NumBytes: c.validity.BinarySize() + c.bodySize(),
	}.WriteTo(w)
	if err != nil {
		return hn, err
	}
	vn, err := c.validity.WriteTo(w)
	if err != nil {
		return hn + vn, err
	}
	bn, err := c.writeBody(w)
	return hn + vn + bn, err
}

func (c *Primitives[T]) width() int {
	return c.pType.ByteWidth()
}

// readPrimitivesFromBuf parses a primitive array body from br. The returned
// Primitives shares backing memory with br.Buf — zero-copy. Callers must keep
// the original buffer alive. Assumes little-endian, x86_64/ARM64 (unaligned
// multi-byte access is supported).
func readPrimitivesFromBuf[T PrimitiveType](br *BufReader, h Header) (*Primitives[T], error) {
	expected := PTypeOfPrimitive[T]()
	if h.PType != expected {
		return nil, fmt.Errorf("array: PType %v does not match %T", h.PType, *new(T))
	}
	width := h.PType.ByteWidth()
	if width == 0 {
		return nil, fmt.Errorf("array: unknown PType %v", h.PType)
	}
	validity := AllValid(h.Length)
	validityBytes := uint64(0)
	if h.Flags&FlagValidity != 0 {
		validityBytes = validityByteLength(h.Length)
		if h.NumBytes < validityBytes {
			return nil, errors.New("array: invalid primitive body size")
		}
		var err error
		validity, err = readValidityFromBuf(br, h.Length)
		if err != nil {
			return nil, err
		}
	}
	if h.Length == 0 {
		if h.NumBytes != validityBytes {
			return nil, errors.New("array: invalid primitive body size")
		}
		return &Primitives[T]{pType: h.PType, data: nil, validity: validity}, nil
	}
	width64 := uint64(width)
	if h.Length > platformSliceLimit()/width64 {
		return nil, errors.New("array: primitive payload exceeds platform limit")
	}
	if h.NumBytes-validityBytes != h.Length*width64 {
		return nil, errors.New("array: invalid primitive body size")
	}
	raw, err := br.Read(int(h.NumBytes - validityBytes))
	if err != nil {
		return nil, fmt.Errorf("array: reading primitive values: %w", err)
	}
	data := unsafe.Slice((*T)(unsafe.Pointer(unsafe.SliceData(raw))), int(h.Length))
	return &Primitives[T]{pType: h.PType, data: data, validity: validity}, nil
}
