package btrblocks

import (
	"fmt"
	"io"
	"math/bits"

	"github.com/axiomhq/btrblocks/array"
)

var (
	_ Codec[uint64] = (*BitpackingCodec[uint64])(nil)
	_ Codec[uint32] = (*BitpackingCodec[uint32])(nil)
	_ Codec[uint16] = (*BitpackingCodec[uint16])(nil)
	_ Codec[uint8]  = (*BitpackingCodec[uint8])(nil)
)

type BitpackingCodec[T UnsignedInteger] struct {
	length   uint64
	bitWidth uint
	buf      []byte
}

func NewBitpackingCodec[T UnsignedInteger](arr array.Array[T]) *BitpackingCodec[T] {
	codec := &BitpackingCodec[T]{length: arr.Length()}
	if arr.Length() == 0 {
		return codec
	}

	var max uint64
	for i := uint64(0); i < arr.Length(); i++ {
		value := arr.ValueAt(i)
		if v := uint64(value); v > max {
			max = v
		}
	}

	codec.bitWidth = uint(bits.Len64(max))
	if codec.bitWidth == 0 {
		return codec
	}

	codec.buf = make([]byte, packedByteSize(codec.length, codec.bitWidth))
	for i := uint64(0); i < arr.Length(); i++ {
		value := arr.ValueAt(i)
		packUnsigned(codec.buf, i*uint64(codec.bitWidth), codec.bitWidth, uint64(value))
	}
	return codec
}

func (c *BitpackingCodec[T]) ValueAt(offset uint64) (T, error) {
	var zero T
	if offset >= c.length {
		return zero, errOffsetOutOfRange
	}
	if c.bitWidth == 0 {
		return zero, nil
	}
	return T(unpackUnsigned(c.buf, offset*uint64(c.bitWidth), c.bitWidth)), nil
}

func (c *BitpackingCodec[T]) Decode(dst []T) error {
	if err := validateDecodeLength(c.length, len(dst)); err != nil {
		return err
	}
	if c.bitWidth == 0 {
		var zero T
		for i := range dst {
			dst[i] = zero
		}
		return nil
	}
	for i := range dst {
		dst[i] = T(unpackUnsigned(c.buf, uint64(i)*uint64(c.bitWidth), c.bitWidth))
	}
	return nil
}

func (c *BitpackingCodec[T]) BinarySize() uint64 { return uint64(headerSize) + c.bodySize() }
func (c *BitpackingCodec[T]) Length() uint64     { return c.length }
func (c *BitpackingCodec[T]) PType() PType       { return pTypeForType[T]() }
func (c *BitpackingCodec[T]) Children() []Scheme { return nil }
func (c *BitpackingCodec[T]) bodySize() uint64   { return 1 + uint64(len(c.buf)) }

func packedByteSize(length uint64, bitWidth uint) int {
	size, err := checkedPackedByteSize(length, bitWidth)
	if err != nil {
		panic(err)
	}
	return size
}

func packUnsigned(buf []byte, bitOffset uint64, bitWidth uint, value uint64) {
	for written := uint(0); written < bitWidth; {
		byteOffset := int(bitOffset / 8)
		bitInByte := uint(bitOffset % 8)
		take := minUint(bitWidth-written, 8-bitInByte)
		chunk := byte((value >> written) & uint64(maskForBits(take)))
		buf[byteOffset] |= chunk << bitInByte
		written += take
		bitOffset += uint64(take)
	}
}

func unpackUnsigned(buf []byte, bitOffset uint64, bitWidth uint) uint64 {
	var value uint64
	for read := uint(0); read < bitWidth; {
		byteOffset := int(bitOffset / 8)
		bitInByte := uint(bitOffset % 8)
		take := minUint(bitWidth-read, 8-bitInByte)
		chunk := (buf[byteOffset] >> bitInByte) & maskForBits(take)
		value |= uint64(chunk) << read
		read += take
		bitOffset += uint64(take)
	}
	return value
}

func maskForBits(width uint) byte {
	if width >= 8 {
		return 0xff
	}
	return byte((uint16(1) << width) - 1)
}

func minUint(a, b uint) uint {
	if a < b {
		return a
	}
	return b
}

func (c *BitpackingCodec[T]) WriteTo(w io.Writer) (n int64, err error) {
	n, err = Header{
		Version:    1,
		Kind:       CodecTypeBitpacking,
		ElemType:   pTypeForType[T](),
		ChildCount: 0,
		Flags:      0,
		Length:     c.length,
		BodySize:   c.bodySize(),
	}.WriteTo(w)
	if err != nil {
		return n, err
	}

	var bitWidth [1]byte
	bitWidth[0] = byte(c.bitWidth)
	nn, err := w.Write(bitWidth[:])
	if err != nil {
		return n + int64(nn), err
	}
	if nn != len(bitWidth) {
		return n + int64(nn), io.ErrShortWrite
	}

	n += int64(nn)

	nn, err = w.Write(c.buf)
	if err != nil {
		return n + int64(nn), err
	}
	if nn != len(c.buf) {
		return n + int64(nn), io.ErrShortWrite
	}
	return n + int64(nn), nil
}

func readAnyBitpackingCodec[T Integer | Float | String](r io.Reader, header Header) (Codec[T], error) {
	var zero T
	switch any(zero).(type) {
	case uint8:
		c, err := readBitpackingCodec[uint8](r, header)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	case uint16:
		c, err := readBitpackingCodec[uint16](r, header)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	case uint32:
		c, err := readBitpackingCodec[uint32](r, header)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	case uint64:
		c, err := readBitpackingCodec[uint64](r, header)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	default:
		return nil, fmt.Errorf("codec: bitpacking not supported for type %v", header.ElemType)
	}
}

func readBitpackingCodec[T UnsignedInteger](r io.Reader, header Header) (Codec[T], error) {
	if header.ChildCount != 0 {
		return nil, fmt.Errorf("codec: bitpacking child count = %d, want 0", header.ChildCount)
	}
	if header.BodySize < 1 {
		return nil, fmt.Errorf("codec: bitpacking body too small")
	}
	var widthByte [1]byte
	if _, err := io.ReadFull(r, widthByte[:]); err != nil {
		return nil, err
	}
	bitWidth := uint(widthByte[0])
	maxBitWidth := uint(pTypeForType[T]().ByteWidth() * 8)
	if maxBitWidth == 0 {
		return nil, fmt.Errorf("codec: bitpacking unsupported for %T", *new(T))
	}
	if bitWidth > maxBitWidth {
		return nil, fmt.Errorf("codec: bitpacking bit width = %d exceeds %T width", bitWidth, *new(T))
	}
	if header.Length == 0 && bitWidth != 0 {
		return nil, fmt.Errorf("codec: bitpacking bit width = %d for empty payload", bitWidth)
	}
	dataSize, err := checkedPackedByteSize(header.Length, bitWidth)
	if err != nil {
		return nil, err
	}
	if header.BodySize != 1+uint64(dataSize) {
		return nil, fmt.Errorf("codec: bitpacking body size = %d, want %d", header.BodySize, 1+uint64(dataSize))
	}
	var buf []byte
	if dataSize > 0 {
		buf = make([]byte, dataSize)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
	}
	return &BitpackingCodec[T]{length: header.Length, bitWidth: bitWidth, buf: buf}, nil
}

func checkedPackedByteSize(length uint64, bitWidth uint) (int, error) {
	if length == 0 || bitWidth == 0 {
		return 0, nil
	}
	maxInt := uint64(^uint(0) >> 1)
	if length > (^uint64(0)-7)/uint64(bitWidth) {
		return 0, fmt.Errorf("codec: bitpacking payload size overflows for length %d and bit width %d", length, bitWidth)
	}
	size := (length*uint64(bitWidth) + 7) / 8
	if size > maxInt {
		return 0, fmt.Errorf("codec: bitpacking payload size %d exceeds maximum", size)
	}
	return int(size), nil
}
