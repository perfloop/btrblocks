package btrblocks

import (
	"fmt"
	"io"
	"math/bits"

	"github.com/axiomhq/btrblocks/array"
)

type bitpackCodec[T UnsignedInteger] struct {
	length   uint64
	bitWidth uint
	buf      []byte
}

func newBitpackCodec[T UnsignedInteger](arr interface {
	Length() uint64
	ValueAt(uint64) T
}) *bitpackCodec[T] {
	codec := &bitpackCodec[T]{length: arr.Length()}
	if arr.Length() == 0 {
		return codec
	}

	var max uint64
	for i := uint64(0); i < arr.Length(); i++ {
		if value := uint64(arr.ValueAt(i)); value > max {
			max = value
		}
	}

	codec.bitWidth = uint(bits.Len64(max))
	if codec.bitWidth == 0 {
		return codec
	}

	codec.buf = make([]byte, packedByteSize(codec.length, codec.bitWidth))
	for i := uint64(0); i < arr.Length(); i++ {
		packUnsigned(codec.buf, i*uint64(codec.bitWidth), codec.bitWidth, uint64(arr.ValueAt(i)))
	}
	return codec
}

func (c *bitpackCodec[T]) Kind() CodeType     { return CodecTypeBitpack }
func (c *bitpackCodec[T]) Length() uint64     { return c.length }
func (c *bitpackCodec[T]) PType() PType       { return pTypeForType[T]() }
func (c *bitpackCodec[T]) BinarySize() uint64 { return uint64(headerSize) + 1 + uint64(len(c.buf)) }

func (c *bitpackCodec[T]) ValueAt(offset uint64) T {
	if offset >= c.length {
		panic(errOffsetOutOfRange)
	}
	if c.bitWidth == 0 {
		var zero T
		return zero
	}
	return T(unpackUnsigned(c.buf, offset*uint64(c.bitWidth), c.bitWidth))
}

func (c *bitpackCodec[T]) Decode(dst []T) error {
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

func (c *bitpackCodec[T]) WriteTo(w io.Writer) (int64, error) {
	n, err := header{
		Version:  versionNumber,
		Kind:     CodecTypeBitpack,
		ElemType: pTypeForType[T](),
		Length:   c.length,
		BodySize: 1 + uint64(len(c.buf)),
	}.WriteTo(w)
	if err != nil {
		return n, err
	}

	var width [1]byte
	width[0] = byte(c.bitWidth)
	nn, err := w.Write(width[:])
	n += int64(nn)
	if err != nil {
		return n, err
	}
	if nn != 1 {
		return n, io.ErrShortWrite
	}

	nn, err = w.Write(c.buf)
	n += int64(nn)
	if err != nil {
		return n, err
	}
	if nn != len(c.buf) {
		return n, io.ErrShortWrite
	}
	return n, nil
}

func readAnyBitpackCodec[T Integer | Float | String](r io.Reader, h header) (Codec[T], error) {
	var zero T
	switch any(zero).(type) {
	case uint8:
		c, err := readBitpackCodec[uint8](r, h)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	case uint16:
		c, err := readBitpackCodec[uint16](r, h)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	case uint32:
		c, err := readBitpackCodec[uint32](r, h)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	case uint64:
		c, err := readBitpackCodec[uint64](r, h)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	default:
		return nil, fmt.Errorf("codec: bitpack not supported for %v", h.ElemType)
	}
}

func readBitpackCodec[T UnsignedInteger](r io.Reader, h header) (Codec[T], error) {
	if h.BodySize < 1 {
		return nil, fmt.Errorf("codec: bitpack body too small")
	}

	var widthByte [1]byte
	if _, err := io.ReadFull(r, widthByte[:]); err != nil {
		return nil, err
	}
	bitWidth := uint(widthByte[0])
	maxBitWidth := uint(pTypeForType[T]().ByteWidth() * 8)
	if bitWidth > maxBitWidth {
		return nil, fmt.Errorf("codec: bit width = %d exceeds %T width", bitWidth, *new(T))
	}

	bufSize, err := checkedPackedByteSize(h.Length, bitWidth)
	if err != nil {
		return nil, err
	}
	if h.BodySize != 1+uint64(bufSize) {
		return nil, fmt.Errorf("codec: bitpack body size = %d, want %d", h.BodySize, 1+uint64(bufSize))
	}

	var buf []byte
	if bufSize > 0 {
		buf = make([]byte, bufSize)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
	}
	return &bitpackCodec[T]{length: h.Length, bitWidth: bitWidth, buf: buf}, nil
}

func packedByteSize(length uint64, bitWidth uint) int {
	size, err := checkedPackedByteSize(length, bitWidth)
	if err != nil {
		panic(err)
	}
	return size
}

func checkedPackedByteSize(length uint64, bitWidth uint) (int, error) {
	if length == 0 || bitWidth == 0 {
		return 0, nil
	}
	maxInt := uint64(^uint(0) >> 1)
	if length > (^uint64(0)-7)/uint64(bitWidth) {
		return 0, fmt.Errorf("codec: payload size overflows for length %d and bit width %d", length, bitWidth)
	}
	size := (length*uint64(bitWidth) + 7) / 8
	if size > maxInt {
		return 0, fmt.Errorf("codec: payload size %d exceeds maximum", size)
	}
	return int(size), nil
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

func estimateBitpack[T UnsignedInteger](arr array.Array[T], _ planContext) (float64, bool) {
	codec := newBitpackCodec(arr)
	if codec.BinarySize() >= rawBinarySize(arr) {
		return 0, false
	}
	return float64(rawBinarySize(arr)) / float64(codec.BinarySize()), true
}
