package array

import (
	"bytes"
	"encoding/binary"
	"errors"
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

// Strings is a columnar array of variable-length strings. Offsets are stored as T (uint8/16/32/64) depending on total byte length; ValueAt is O(1).
type Strings[T UnsignedInteger] struct {
	offsets []T // length+1 offsets; offsets[i]..offsets[i+1] is the i-th string in buf.
	buf     []byte
}

// NewStrings builds a string array from values, choosing the smallest offset type that can represent the total byte length.
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

// newStringsWithOffsets constructs a Strings array with the given offset type T. Caller must ensure total fits in T.
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

func (c *Strings[T]) BinarySize() uint64 { return uint64(headerSize) + c.bodySize() }
func (c *Strings[T]) Length() uint64     { return uint64(len(c.offsets) - 1) }
func (c *Strings[T]) PType() PType       { return PTypeString }

// bodySize returns the size in bytes of the string body: 4-byte buf length + offsets + raw string bytes.
func (c *Strings[T]) bodySize() uint64 {
	return 4 + uint64(len(c.buf)) + uint64(len(c.offsets))*uint64(unsafe.Sizeof(T(0)))
}

func (c *Strings[T]) header() Header {
	return Header{
		Version:  1,
		PType:    PTypeString,
		Length:   c.Length(),
		BodySize: c.bodySize(),
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

func readStringsWithHeader(r io.Reader, h Header) (Array[string], error) {
	if h.PType != PTypeString {
		return nil, errors.New("array: not a string array")
	}
	if h.Length > maxArrayLength {
		return nil, errors.New("array: string array length exceeds maximum")
	}
	var bufLen uint32
	if err := binary.Read(r, binary.LittleEndian, &bufLen); err != nil {
		return nil, err
	}
	if bufLen > maxStringBufLen {
		return nil, errors.New("array: string buffer length exceeds maximum")
	}
	offsetsSize := int(h.BodySize) - 4 - int(bufLen)
	numOffsets := int(h.Length) + 1
	if numOffsets <= 0 || offsetsSize <= 0 {
		return nil, errors.New("array: invalid string body")
	}
	if numOffsets > maxArrayLength+1 {
		return nil, errors.New("array: string array length exceeds maximum")
	}
	if offsetsSize%numOffsets != 0 {
		return nil, errors.New("array: invalid string offsets layout")
	}
	width := offsetsSize / numOffsets
	switch width {
	case 1:
		return readStringsOffsets[uint8](r, numOffsets, bufLen)
	case 2:
		return readStringsOffsets[uint16](r, numOffsets, bufLen)
	case 4:
		return readStringsOffsets[uint32](r, numOffsets, bufLen)
	case 8:
		return readStringsOffsets[uint64](r, numOffsets, bufLen)
	default:
		return nil, errors.New("array: unsupported string offset width")
	}
}

// ReadStrings reads a string array from r. The offset width is inferred from the body; the returned Array[string] is the appropriate Strings[T] (uint8/uint16/uint32/uint64).
func ReadStrings(r io.Reader) (Array[string], error) {
	h, err := readHeader(r)
	if err != nil {
		return nil, err
	}
	return readStringsWithHeader(r, h)
}

// readStringsOffsets reads numOffsets offsets of type T and bufLen bytes of string data, then returns a Strings[T].
func readStringsOffsets[T UnsignedInteger](r io.Reader, numOffsets int, bufLen uint32) (*Strings[T], error) {
	width := int(unsafe.Sizeof(T(0)))
	offsetBytes := make([]byte, numOffsets*width)
	if _, err := io.ReadFull(r, offsetBytes); err != nil {
		return nil, err
	}
	offsets := make([]T, numOffsets)
	for i := range offsets {
		if err := binary.Read(bytes.NewReader(offsetBytes[i*width:(i+1)*width]), binary.LittleEndian, &offsets[i]); err != nil {
			return nil, err
		}
	}
	if err := validateStringOffsets(offsets, bufLen); err != nil {
		return nil, err
	}
	buf := make([]byte, bufLen)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return &Strings[T]{offsets: offsets, buf: buf}, nil
}

func validateStringOffsets[T UnsignedInteger](offsets []T, bufLen uint32) error {
	if len(offsets) == 0 {
		return errors.New("array: invalid string offsets")
	}
	if offsets[0] != 0 {
		return errors.New("array: string offsets must start at 0")
	}
	for i := 1; i < len(offsets); i++ {
		if offsets[i] < offsets[i-1] {
			return errors.New("array: string offsets must be monotonic")
		}
	}
	if uint64(offsets[len(offsets)-1]) != uint64(bufLen) {
		return errors.New("array: string offsets must end at buffer length")
	}
	return nil
}
