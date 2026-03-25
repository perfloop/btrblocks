package array

import (
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
	total, err := totalStringBytes(values)
	if err != nil {
		panic(err)
	}
	switch {
	case total <= math.MaxUint8:
		return newStringsWithOffsets[uint8](values, total)
	case total <= math.MaxUint16:
		return newStringsWithOffsets[uint16](values, total)
	default:
		return newStringsWithOffsets[uint32](values, total)
	}
}

// newStringsWithOffsets constructs a Strings array with the given offset type T. Caller must ensure total fits in T.
func newStringsWithOffsets[T UnsignedInteger](values []string, total uint64) *Strings[T] {
	buf := make([]byte, int(total))
	offsets := make([]T, len(values)+1)
	var pos uint64
	offsets[0] = 0
	for i, v := range values {
		copy(buf[int(pos):], v)
		pos += uint64(len(v))
		offsets[i+1] = T(pos)
	}
	return &Strings[T]{
		offsets: offsets,
		buf:     buf,
	}
}

func totalStringBytes(values []string) (uint64, error) {
	var total uint64
	for _, v := range values {
		size := uint64(len(v))
		if total > ^uint64(0)-size {
			return 0, errors.New("array: string data size overflows")
		}
		total += size
	}
	if err := validateStringDataSize(total); err != nil {
		return 0, err
	}
	return total, nil
}

func validateStringDataSize(total uint64) error {
	if total > math.MaxUint32 {
		return errors.New("array: string data exceeds format limit")
	}
	if total > platformSliceLimit() {
		return errors.New("array: string data exceeds platform limit")
	}
	return nil
}

func (c *Strings[T]) ValueAt(offset uint64) string {
	start := int(c.offsets[offset])
	end := int(c.offsets[offset+1])
	if start == end {
		return ""
	}
	buf := c.buf[start:end]
	return unsafe.String(unsafe.SliceData(buf), len(buf))
}

func (c *Strings[T]) CopyTo(dst []string) {
	for i := range dst {
		dst[i] = c.ValueAt(uint64(i))
	}
}

func (c *Strings[T]) Slice(start, end uint64) (Array[string], error) {
	if err := ValidateSliceBounds(c.Length(), start, end); err != nil {
		return nil, err
	}
	base := c.offsets[start]
	bufStart := int(base)
	bufEnd := int(c.offsets[end])
	offsets := make([]T, int(end-start)+1)
	for i := range offsets {
		offsets[i] = c.offsets[start+uint64(i)] - base
	}
	return &Strings[T]{
		offsets: offsets,
		buf:     c.buf[bufStart:bufEnd],
	}, nil
}

func (c *Strings[T]) BinarySize() uint64 { return uint64(headerSize) + c.bodySize() }
func (c *Strings[T]) Length() uint64     { return uint64(len(c.offsets) - 1) }
func (c *Strings[T]) PType() PType       { return PTypeString }
func (c *Strings[T]) Buffer() []byte     { return c.buf }
func (c *Strings[T]) Offsets() []T       { return c.offsets }

// bodySize returns the size in bytes of the string body: 4-byte buf length + offsets + raw string bytes.
func (c *Strings[T]) bodySize() uint64 {
	return 4 + uint64(len(c.buf)) + uint64(len(c.offsets))*uint64(unsafe.Sizeof(T(0)))
}

func (c *Strings[T]) header() Header {
	return Header{
		Version: 1,
		PType:   PTypeString,
		Length:  c.Length(),
		NumBytes:  c.bodySize(),
	}
}

func (c *Strings[T]) writeBody(w io.Writer) (int64, error) {
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], uint32(len(c.buf)))
	wn, err := w.Write(lenBuf[:])
	if err != nil {
		return int64(wn), err
	}
	if wn != len(lenBuf) {
		return int64(wn), io.ErrShortWrite
	}
	n := int64(4)

	if len(c.offsets) > 0 {
		width := int(unsafe.Sizeof(T(0)))
		byteLen := len(c.offsets) * width
		b := unsafe.Slice((*byte)(unsafe.Pointer(&c.offsets[0])), byteLen)
		wn, err = w.Write(b)
		if err != nil {
			return n + int64(wn), err
		}
		if wn != byteLen {
			return n + int64(wn), io.ErrShortWrite
		}
		n += int64(wn)
	}
	wn, err = w.Write(c.buf)
	if err == nil && wn != len(c.buf) {
		err = io.ErrShortWrite
	}
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
	if h.NumBytes < 5 || h.Length == ^uint64(0) {
		return nil, errors.New("array: invalid string body")
	}
	numOffsets := h.Length + 1
	var bufLen uint32
	if err := binary.Read(r, binary.LittleEndian, &bufLen); err != nil {
		return nil, err
	}
	if uint64(bufLen)+4 > h.NumBytes {
		return nil, errors.New("array: invalid string body")
	}
	offsetsSize := h.NumBytes - 4 - uint64(bufLen)
	if offsetsSize == 0 {
		return nil, errors.New("array: invalid string body")
	}
	if offsetsSize%numOffsets != 0 {
		return nil, errors.New("array: invalid string offsets layout")
	}
	width := offsetsSize / numOffsets
	if numOffsets > platformSliceLimit() || offsetsSize > platformSliceLimit() || uint64(bufLen) > platformSliceLimit() {
		return nil, errors.New("array: string payload exceeds platform limit")
	}
	switch width {
	case 1:
		return readStringsOffsets[uint8](r, int(numOffsets), bufLen)
	case 2:
		return readStringsOffsets[uint16](r, int(numOffsets), bufLen)
	case 4:
		return readStringsOffsets[uint32](r, int(numOffsets), bufLen)
	case 8:
		return readStringsOffsets[uint64](r, int(numOffsets), bufLen)
	default:
		return nil, errors.New("array: unsupported string offset width")
	}
}

// ReadStrings reads a string array from r. The offset width is inferred from the body; the returned Array[string] is the appropriate Strings[T] (uint8/uint16/uint32/uint64).
func ReadStrings(r io.Reader, opts ...ReadOptions) (Array[string], error) {
	h, err := readHeader(r)
	if err != nil {
		return nil, err
	}
	if err := validateHeader(h, readOpts(opts)); err != nil {
		return nil, err
	}
	return readStringsWithHeader(r, h)
}

// readStringsOffsets reads numOffsets offsets of type T and bufLen bytes of string data, then returns a Strings[T].
func readStringsOffsets[T UnsignedInteger](r io.Reader, numOffsets int, bufLen uint32) (*Strings[T], error) {
	offsets := make([]T, numOffsets)
	if numOffsets > 0 {
		width := int(unsafe.Sizeof(T(0))) // nasty but cool :D
		byteLen := numOffsets * width
		b := unsafe.Slice((*byte)(unsafe.Pointer(&offsets[0])), byteLen)
		if _, err := io.ReadFull(r, b); err != nil {
			return nil, err
		}
	}
	if err := validateStringOffsets(offsets, bufLen); err != nil {
		return nil, err
	}
	buf := make([]byte, int(bufLen))
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return &Strings[T]{offsets: offsets, buf: buf}, nil
}

func readStringsFromBuf(br *BufReader, h Header) (Array[string], error) {
	if h.PType != PTypeString {
		return nil, errors.New("array: not a string array")
	}
	if h.NumBytes < 5 || h.Length == ^uint64(0) {
		return nil, errors.New("array: invalid string body")
	}
	numOffsets := h.Length + 1
	bufLenBytes, err := br.Read(4)
	if err != nil {
		return nil, err
	}
	bufLen := binary.LittleEndian.Uint32(bufLenBytes)
	if uint64(bufLen)+4 > h.NumBytes {
		return nil, errors.New("array: invalid string body")
	}
	offsetsSize := h.NumBytes - 4 - uint64(bufLen)
	if offsetsSize == 0 {
		return nil, errors.New("array: invalid string body")
	}
	if offsetsSize%numOffsets != 0 {
		return nil, errors.New("array: invalid string offsets layout")
	}
	width := offsetsSize / numOffsets
	if numOffsets > platformSliceLimit() || offsetsSize > platformSliceLimit() || uint64(bufLen) > platformSliceLimit() {
		return nil, errors.New("array: string payload exceeds platform limit")
	}
	switch width {
	case 1:
		return readStringsOffsetsFromBuf[uint8](br, int(numOffsets), bufLen)
	case 2:
		return readStringsOffsetsFromBuf[uint16](br, int(numOffsets), bufLen)
	case 4:
		return readStringsOffsetsFromBuf[uint32](br, int(numOffsets), bufLen)
	case 8:
		return readStringsOffsetsFromBuf[uint64](br, int(numOffsets), bufLen)
	default:
		return nil, errors.New("array: unsupported string offset width")
	}
}

func readStringsOffsetsFromBuf[T UnsignedInteger](br *BufReader, numOffsets int, bufLen uint32) (*Strings[T], error) {
	width := int(unsafe.Sizeof(T(0)))
	byteLen := numOffsets * width
	raw, err := br.Read(byteLen)
	if err != nil {
		return nil, err
	}
	offsets := unsafe.Slice((*T)(unsafe.Pointer(unsafe.SliceData(raw))), numOffsets)
	if err := validateStringOffsets(offsets, bufLen); err != nil {
		return nil, err
	}
	buf, err := br.Read(int(bufLen))
	if err != nil {
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
