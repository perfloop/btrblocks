//go:build 386 || amd64 || arm || arm64 || loong64 || mips64le || mipsle || ppc64le || riscv64 || wasm

package array

import (
	"encoding/binary"
	"errors"
	"fmt"
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
	offsets  []T // length+1 offsets; offsets[i]..offsets[i+1] is the i-th string in buf.
	buf      []byte
	validity Validity
}

// NewStrings builds a string array from values, choosing the smallest offset
// type that can represent the total byte length.
func NewStrings(values []string) (Array[string], error) {
	return NewStringsWithValidity(values, AllValid(uint64(len(values))))
}

// NewStringsWithNulls builds a string array from values. A true entry in
// nulls marks the corresponding value as null. An empty nulls slice means all
// values are valid; otherwise its length must equal len(values).
func NewStringsWithNulls(values []string, nulls []bool) (Array[string], error) {
	validity, err := ValidityFromNulls(uint64(len(values)), nulls)
	if err != nil {
		return nil, fmt.Errorf("array: string nulls: %w", err)
	}
	return NewStringsWithValidity(values, validity)
}

// NewStringsWithValidity copies values into a string array with native,
// immutable validity metadata.
func NewStringsWithValidity(values []string, validity Validity) (Array[string], error) {
	if validity.Length() != uint64(len(values)) {
		return nil, fmt.Errorf("array: validity length = %d, want %d", validity.Length(), len(values))
	}
	total, err := totalStringBytes(values)
	if err != nil {
		return nil, err
	}
	switch {
	case total <= math.MaxUint8:
		return newStringsWithOffsets[uint8](values, total, validity), nil
	case total <= math.MaxUint16:
		return newStringsWithOffsets[uint16](values, total, validity), nil
	default:
		return newStringsWithOffsets[uint32](values, total, validity), nil
	}
}

// newStringsWithOffsets constructs a Strings array with the given offset type T. Caller must ensure total fits in T.
func newStringsWithOffsets[T UnsignedInteger](values []string, total uint64, validity Validity) *Strings[T] {
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
		offsets:  offsets,
		buf:      buf,
		validity: validity,
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

func (c *Strings[T]) IsValid(offset uint64) bool { return c.validity.IsValid(offset) }

// Validity returns the immutable validity metadata for c.
func (c *Strings[T]) Validity() Validity { return c.validity }
func (c *Strings[T]) NullCount() uint64  { return c.validity.NullCount() }

func (c *Strings[T]) CopyTo(dst []string) {
	valueLen := len(c.offsets)
	if valueLen == 0 {
		return
	}
	valueLen--
	for i := range min(len(dst), valueLen) {
		dst[i] = c.ValueAt(uint64(i))
	}
}

func (c *Strings[T]) Slice(start, end uint64) (Array[string], error) {
	if err := ValidateSliceBounds(c.Length(), start, end); err != nil {
		return nil, err
	}
	if len(c.offsets) == 0 {
		return &Strings[T]{offsets: []T{0}, validity: AllValid(0)}, nil
	}
	base := c.offsets[start]
	bufStart := int(base)
	bufEnd := int(c.offsets[end])
	offsets := make([]T, int(end-start)+1)
	for i := range offsets {
		offsets[i] = c.offsets[start+uint64(i)] - base
	}
	validity, err := c.validity.Slice(start, end)
	if err != nil {
		return nil, err
	}
	return &Strings[T]{
		offsets:  offsets,
		buf:      c.buf[bufStart:bufEnd],
		validity: validity,
	}, nil
}

func (c *Strings[T]) BinarySize() uint64 {
	return uint64(headerSize) + c.validity.BinarySize() + c.bodySize()
}
func (c *Strings[T]) Length() uint64 {
	if len(c.offsets) == 0 {
		return 0
	}
	return uint64(len(c.offsets) - 1)
}
func (c *Strings[T]) PType() PType { return PTypeString }

// BufferUnsafe returns borrowed, read-only string storage. The slice is valid
// only while c is alive and must not be modified.
func (c *Strings[T]) BufferUnsafe() []byte { return c.buf }

// OffsetsUnsafe returns borrowed, read-only offsets. The slice is valid only
// while c is alive and must not be modified.
func (c *Strings[T]) OffsetsUnsafe() []T { return c.offsets }

// bodySize returns the size in bytes of the string body: 4-byte buf length + offsets + raw string bytes.
func (c *Strings[T]) bodySize() uint64 {
	numOffsets := len(c.offsets)
	if numOffsets == 0 {
		numOffsets = 1
	}
	return 4 + uint64(len(c.buf)) + uint64(numOffsets)*uint64(unsafe.Sizeof(T(0)))
}

func (c *Strings[T]) header() Header {
	flags := uint16(0)
	if c.NullCount() != 0 {
		flags |= FlagValidity
	}
	return Header{
		Version:  FormatVersion,
		PType:    PTypeString,
		Flags:    flags,
		Length:   c.Length(),
		NumBytes: c.validity.BinarySize() + c.bodySize(),
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

	width := int(unsafe.Sizeof(T(0)))
	if len(c.offsets) > 0 {
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
	} else {
		var zero T
		b := unsafe.Slice((*byte)(unsafe.Pointer(&zero)), width)
		wn, err = w.Write(b)
		if err != nil {
			return n + int64(wn), err
		}
		if wn != width {
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
	vn, err := c.validity.WriteTo(w)
	if err != nil {
		return hn + vn, err
	}
	bn, err := c.writeBody(w)
	return hn + vn + bn, err
}

func readStringsFromBuf(br *BufReader, h Header) (Array[string], error) {
	if h.PType != PTypeString {
		return nil, errors.New("array: not a string array")
	}
	validity := AllValid(h.Length)
	validityBytes := uint64(0)
	if h.Flags&FlagValidity != 0 {
		validityBytes = validityByteLength(h.Length)
		if h.NumBytes < validityBytes {
			return nil, errors.New("array: invalid string body")
		}
		var err error
		validity, err = readValidityFromBuf(br, h.Length)
		if err != nil {
			return nil, err
		}
	}
	bodyBytes := h.NumBytes - validityBytes
	if bodyBytes < 5 || h.Length == ^uint64(0) {
		return nil, errors.New("array: invalid string body")
	}
	numOffsets := h.Length + 1
	bufLenBytes, err := br.Read(4)
	if err != nil {
		return nil, fmt.Errorf("array: reading string buffer length: %w", err)
	}
	bufLen := binary.LittleEndian.Uint32(bufLenBytes)
	if uint64(bufLen)+4 > bodyBytes {
		return nil, errors.New("array: invalid string body")
	}
	offsetsSize := bodyBytes - 4 - uint64(bufLen)
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
		return readStringsOffsetsFromBuf[uint8](br, int(numOffsets), bufLen, validity)
	case 2:
		return readStringsOffsetsFromBuf[uint16](br, int(numOffsets), bufLen, validity)
	case 4:
		return readStringsOffsetsFromBuf[uint32](br, int(numOffsets), bufLen, validity)
	case 8:
		return readStringsOffsetsFromBuf[uint64](br, int(numOffsets), bufLen, validity)
	default:
		return nil, errors.New("array: unsupported string offset width")
	}
}

func readStringsOffsetsFromBuf[T UnsignedInteger](br *BufReader, numOffsets int, bufLen uint32, validity Validity) (*Strings[T], error) {
	width := int(unsafe.Sizeof(T(0)))
	byteLen := numOffsets * width
	raw, err := br.Read(byteLen)
	if err != nil {
		return nil, fmt.Errorf("array: reading string offsets: %w", err)
	}
	offsets := unsafe.Slice((*T)(unsafe.Pointer(unsafe.SliceData(raw))), numOffsets)
	if err := validateStringOffsets(offsets, bufLen); err != nil {
		return nil, err
	}
	buf, err := br.Read(int(bufLen))
	if err != nil {
		return nil, fmt.Errorf("array: reading string bytes: %w", err)
	}
	return &Strings[T]{offsets: offsets, buf: buf, validity: validity}, nil
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
