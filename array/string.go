package array

import (
	"io"
	"math"
	"unsafe"
)

var (
	_ Array[string] = (*Strings[uint8])(nil)
	_ Array[string] = (*Strings[uint16])(nil)
	_ Array[string] = (*Strings[uint32])(nil)
	_ Array[string] = (*Strings[uint64])(nil)
)

type Strings[T UnsignedInteger] struct {
	offsets []T
	buf     []byte
}

func NewStrings(values []string) Array[string] {
	total := 0
	for _, v := range values {
		total += len(v)
	}
	switch {
	case total <= math.MaxUint8:
		return newStringsWithOffsets[uint8](values, total, 1)
	case total <= math.MaxUint16:
		return newStringsWithOffsets[uint16](values, total, 2)
	case total <= math.MaxUint32:
		return newStringsWithOffsets[uint32](values, total, 4)
	default:
		return newStringsWithOffsets[uint64](values, total, 8)
	}
}

func newStringsWithOffsets[T UnsignedInteger](values []string, total int, width int) *Strings[T] {
	buf := make([]byte, total)
	offsets := make([]T, len(values)+1)
	pos := 0
	offsets[0] = 0
	for i, v := range values {
		copy(buf[pos:], v)
		pos += len(v)
		offsets[i+1] = T(pos)
	}
	return &Strings[T]{
		offsets: offsets,
		buf:     buf,
	}
}

func (c *Strings[T]) ValueAt(offset uint64) string {
	return string(c.buf[c.offsets[offset]:c.offsets[offset+1]])
}

func (c *Strings[T]) BinarySize() uint64 {
	return uint64(len(c.buf)) + uint64(len(c.offsets))*uint64(unsafe.Sizeof(T(0)))
}

func (c *Strings[T]) WriteTo(w io.Writer) (int64, error) { return int64(len(c.buf)), nil }
func (c *Strings[T]) Length() uint64                     { return uint64(len(c.offsets)) }
func (c *Strings[T]) PType() PType                       { return PTypeString }
