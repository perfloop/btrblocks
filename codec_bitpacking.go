package btrblocks

import (
	"io"
	"math/bits"
)

type bitpackableUnsigned interface {
	~uint8 | ~uint16 | ~uint32 | ~uint64
}

var (
	_ Codec[uint64] = (*BitpackingCodec[uint64])(nil)
	_ Codec[uint32] = (*BitpackingCodec[uint32])(nil)
	_ Codec[uint16] = (*BitpackingCodec[uint16])(nil)
	_ Codec[uint8]  = (*BitpackingCodec[uint8])(nil)
)

type BitpackingCodec[T bitpackableUnsigned] struct {
	length   uint64
	bitWidth uint
	buf      []byte
}

func NewBitpackingCodec[T bitpackableUnsigned](data []T) *BitpackingCodec[T] {
	codec := &BitpackingCodec[T]{length: uint64(len(data))}
	if len(data) == 0 {
		return codec
	}

	var max uint64
	for _, value := range data {
		if v := uint64(value); v > max {
			max = v
		}
	}

	codec.bitWidth = uint(bits.Len64(max))
	if codec.bitWidth == 0 {
		return codec
	}

	codec.buf = make([]byte, packedByteSize(codec.length, codec.bitWidth))
	for i, value := range data {
		packUnsigned(codec.buf, uint64(i)*uint64(codec.bitWidth), codec.bitWidth, uint64(value))
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

func (c *BitpackingCodec[T]) BinarySize() uint64 { return uint64(headerSize) + c.bodySize() }
func (c *BitpackingCodec[T]) Length() uint64     { return c.length }
func (c *BitpackingCodec[T]) PType() PType       { return pTypeForType[T]() }
func (c *BitpackingCodec[T]) Children() []Scheme { return nil }
func (c *BitpackingCodec[T]) bodySize() uint64   { return 1 + uint64(len(c.buf)) }

func packedByteSize(length uint64, bitWidth uint) int {
	if length == 0 || bitWidth == 0 {
		return 0
	}
	return int((length*uint64(bitWidth) + 7) / 8)
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
		return n, err
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
