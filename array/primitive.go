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
	pType PType
	data  []T
}

// NewPrimitives builds a Primitives array from a copy of values. The returned array does not share storage with the input.
func NewPrimitives[T PrimitiveType](values []T) *Primitives[T] {
	data := make([]T, len(values))
	copy(data, values)
	return NewPrimitivesUnsafe(data)
}

// NewPrimitivesUnsafe builds a Primitives array that uses the given slice as its backing storage. The caller must not modify the slice after construction.
func NewPrimitivesUnsafe[T PrimitiveType](data []T) *Primitives[T] {
	pType := PTypeForType[T]()
	return &Primitives[T]{pType: pType, data: data}
}

func (c *Primitives[T]) ValueAt(offset uint64) T { return c.data[offset] }
func (c *Primitives[T]) CopyTo(dst []T)          { copy(dst, c.data) }
func (c *Primitives[T]) BinarySize() uint64      { return uint64(headerSize) + c.bodySize() }
func (c *Primitives[T]) Length() uint64          { return uint64(len(c.data)) }
func (c *Primitives[T]) PType() PType            { return c.pType }
func (c *Primitives[T]) bodySize() uint64        { return uint64(len(c.data)) * uint64(c.width()) }

func (c *Primitives[T]) Slice(start, end uint64) (Array[T], error) {
	if err := ValidateSliceBounds(c.Length(), start, end); err != nil {
		return nil, err
	}
	return &Primitives[T]{pType: c.pType, data: c.data[int(start):int(end)]}, nil
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
	hn, err := Header{
		Version: 1,
		PType:   c.pType,
		Length:  c.Length(),
		NBytes:  c.bodySize(),
	}.WriteTo(w)
	if err != nil {
		return hn, err
	}
	bn, err := c.writeBody(w)
	return hn + bn, err
}

func (c *Primitives[T]) width() int {
	return c.pType.ByteWidth()
}

func readPrimitivesWithHeader[T PrimitiveType](r io.Reader, h Header) (*Primitives[T], error) {
	expected := PTypeForType[T]()
	if h.PType != expected {
		return nil, fmt.Errorf("array: PType %v does not match %T", h.PType, *new(T))
	}
	width := h.PType.ByteWidth()
	if width == 0 {
		return nil, fmt.Errorf("array: unknown PType %v", h.PType)
	}
	if h.Length == 0 {
		if h.NBytes != 0 {
			return nil, errors.New("array: invalid primitive body size")
		}
		return &Primitives[T]{pType: h.PType, data: nil}, nil
	}
	width64 := uint64(width)
	if h.Length > platformSliceLimit()/width64 {
		return nil, errors.New("array: primitive payload exceeds platform limit")
	}
	if h.NBytes != h.Length*width64 {
		return nil, errors.New("array: invalid primitive body size")
	}
	n := int(h.Length)
	bodySize := int(h.NBytes)
	data := make([]T, n)
	b := unsafe.Slice((*byte)(unsafe.Pointer(&data[0])), bodySize)
	if _, err := io.ReadFull(r, b); err != nil {
		return nil, err
	}
	return &Primitives[T]{pType: h.PType, data: data}, nil
}

// ReadPrimitives reads a primitive array from r. The type parameter T must match the array's PType; otherwise an error is returned.
func ReadPrimitives[T PrimitiveType](r io.Reader) (*Primitives[T], error) {
	h, err := readHeader(r)
	if err != nil {
		return nil, err
	}
	if err := validateHeader(h); err != nil {
		return nil, err
	}
	return readPrimitivesWithHeader[T](r, h)
}
