package array

import (
	"encoding/binary"
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

func (c *Strings[T]) Length() uint64 { return uint64(len(c.offsets) - 1) }
func (c *Strings[T]) PType() PType   { return PTypeString }

func (c *Strings[T]) header() Header {
	return Header{
		Version:  1,
		PType:    PTypeString,
		Length:   c.Length(),
		BodySize: 4 + c.BinarySize(),
	}
}

func (c *Strings[T]) writeBody(w io.Writer) (int64, error) {
	if err := binary.Write(w, binary.LittleEndian, uint32(len(c.buf))); err != nil {
		return 0, err
	}
	n := int64(4)

	var buf [8]byte
	width := int(unsafe.Sizeof(T(0)))
	for _, o := range c.offsets {
		switch v := any(o).(type) {
		case uint8:
			buf[0] = v
		case uint16:
			binary.LittleEndian.PutUint16(buf[:2], v)
		case uint32:
			binary.LittleEndian.PutUint32(buf[:4], v)
		case uint64:
			binary.LittleEndian.PutUint64(buf[:8], v)
		}
		wn, err := w.Write(buf[:width])
		if err != nil {
			return n + int64(wn), err
		}
		n += int64(wn)
	}
	wn, err := w.Write(c.buf)
	return n + int64(wn), err
}

func (c *Strings[T]) WriteTo(w io.Writer) (int64, error) {
	hn, err := c.header().WriteTo(w)
	if err != nil {
		return hn, err
	}
	bn, err := c.writeBody(w)
	return hn + bn, err
}
