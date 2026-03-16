package btrblocks

import (
	"fmt"
	"io"
	"unsafe"

	"github.com/axiomhq/btrblocks/array"
)

type ZigzagCodec[T SignedInteger, U UnsignedInteger] struct {
	data Codec[U]
}

// primitiveArrayHeaderSize mirrors array/header.go.
const primitiveArrayHeaderSize = 20

type zigzagEncodedArray[T SignedInteger, U UnsignedInteger] struct {
	length  uint64
	valueAt func(uint64) T
}

func (a zigzagEncodedArray[T, U]) encodedAt(offset uint64) U {
	return U(zigzagEncodeValue(a.valueAt(offset)))
}

func (a zigzagEncodedArray[T, U]) ValueAt(offset uint64) U {
	return a.encodedAt(offset)
}

func (a zigzagEncodedArray[T, U]) CopyTo(dst []U) {
	for i := range dst {
		dst[i] = a.encodedAt(uint64(i))
	}
}

func (a zigzagEncodedArray[T, U]) BinarySize() uint64 {
	return primitiveArrayHeaderSize + a.length*uint64(pTypeForType[U]().ByteWidth())
}

func (a zigzagEncodedArray[T, U]) Length() uint64 {
	return a.length
}

func (a zigzagEncodedArray[T, U]) PType() PType {
	return pTypeForType[U]()
}

func (a zigzagEncodedArray[T, U]) WriteTo(w io.Writer) (int64, error) {
	bodySize := a.length * uint64(pTypeForType[U]().ByteWidth())
	n, err := array.Header{
		Version:  1,
		PType:    array.PTypeForType[U](),
		Length:   a.length,
		BodySize: bodySize,
	}.WriteTo(w)
	if err != nil {
		return n, err
	}
	if a.length == 0 {
		return n, nil
	}

	const chunkElems = 1024
	buf := make([]U, chunkElems)
	width := int(unsafe.Sizeof(U(0)))
	var written int64
	for offset := uint64(0); offset < a.length; {
		chunk := len(buf)
		remaining := a.length - offset
		if remaining < uint64(chunk) {
			chunk = int(remaining)
		}
		for i := 0; i < chunk; i++ {
			buf[i] = a.encodedAt(offset + uint64(i))
		}
		bytes := unsafe.Slice((*byte)(unsafe.Pointer(&buf[0])), chunk*width)
		wn, err := w.Write(bytes)
		written += int64(wn)
		if err != nil {
			return n + written, err
		}
		if wn != len(bytes) {
			return n + written, io.ErrShortWrite
		}
		offset += uint64(chunk)
	}
	return n + written, nil
}

func zigzagEncode64(n int64) uint64 {
	return uint64((n << 1) ^ (n >> 63))
}

func zigzagDecode64(z uint64) int64 {
	return int64(z>>1) ^ -int64(z&1)
}

func zigzagEncodeValue[T SignedInteger](v T) uint64 {
	switch value := any(v).(type) {
	case int8:
		return zigzagEncode64(int64(value))
	case int16:
		return zigzagEncode64(int64(value))
	case int32:
		return zigzagEncode64(int64(value))
	case int64:
		return zigzagEncode64(value)
	default:
		return 0
	}
}

func zigzagMaxEncodedFromSource[T SignedInteger](length uint64, valueAt func(uint64) T) uint64 {
	var max uint64
	for i := uint64(0); i < length; i++ {
		encoded := zigzagEncodeValue(valueAt(i))
		if encoded > max {
			max = encoded
		}
	}
	return max
}

func newZigzagCodecWithWidthFromSource[T SignedInteger, U UnsignedInteger](length uint64, valueAt func(uint64) T, depth int) (Codec[T], error) {
	inner := CompressInteger(zigzagEncodedArray[T, U]{length: length, valueAt: valueAt}, depth-1)
	return &ZigzagCodec[T, U]{data: inner}, nil
}

func NewZigzagCodec[T SignedInteger](vals []T, depth int) (Codec[T], error) {
	return newZigzagCodecFromSource(uint64(len(vals)), func(i uint64) T { return vals[i] }, depth)
}

func newZigzagCodecFromArray[T SignedInteger](arr array.Array[T], depth int) (Codec[T], error) {
	return newZigzagCodecFromSource(arr.Length(), arr.ValueAt, depth)
}

func newZigzagCodecFromSource[T SignedInteger](length uint64, valueAt func(uint64) T, depth int) (Codec[T], error) {
	if depth <= 0 {
		return nil, errDepthExhausted
	}
	max := zigzagMaxEncodedFromSource(length, valueAt)
	switch {
	case max <= uint64(^uint8(0)):
		return newZigzagCodecWithWidthFromSource[T, uint8](length, valueAt, depth)
	case max <= uint64(^uint16(0)):
		return newZigzagCodecWithWidthFromSource[T, uint16](length, valueAt, depth)
	case max <= uint64(^uint32(0)):
		return newZigzagCodecWithWidthFromSource[T, uint32](length, valueAt, depth)
	default:
		return newZigzagCodecWithWidthFromSource[T, uint64](length, valueAt, depth)
	}
}

func (z *ZigzagCodec[T, U]) ValueAt(offset uint64) (T, error) {
	var zero T
	u, err := z.data.ValueAt(offset)
	if err != nil {
		return zero, err
	}
	value := uint64(u)
	switch any(zero).(type) {
	case int8:
		return any(int8(zigzagDecode64(value))).(T), nil
	case int16:
		return any(int16(zigzagDecode64(value))).(T), nil
	case int32:
		return any(int32(zigzagDecode64(value))).(T), nil
	case int64:
		return any(zigzagDecode64(value)).(T), nil
	default:
		return zero, fmt.Errorf("codec: zigzag not supported for type %T", zero)
	}
}

func (z *ZigzagCodec[T, U]) Decode(dst []T) error {
	if err := validateDecodeLength(z.data.Length(), len(dst)); err != nil {
		return err
	}
	dataScratch, haveDataScratch, err := decodeWithOptionalScratch(z.data, nil)
	if err != nil {
		return err
	}
	if haveDataScratch {
		for i, u := range dataScratch {
			dst[i] = T(zigzagDecode64(uint64(u)))
		}
		return nil
	}
	for i := range dst {
		u, err := z.data.ValueAt(uint64(i))
		if err != nil {
			return err
		}
		dst[i] = T(zigzagDecode64(uint64(u)))
	}
	return nil
}

func (z *ZigzagCodec[T, U]) Children() []Scheme { return []Scheme{z.data} }
func (z *ZigzagCodec[T, U]) BinarySize() uint64 { return uint64(headerSize) + z.data.BinarySize() }
func (z *ZigzagCodec[T, U]) Length() uint64     { return z.data.Length() }
func (z *ZigzagCodec[T, U]) PType() PType       { return pTypeForType[T]() }

func (z *ZigzagCodec[T, U]) WriteTo(w io.Writer) (n int64, err error) {
	n, err = Header{
		Version:    1,
		Kind:       CodecTypeZigzag,
		ElemType:   pTypeForType[T](),
		ChildCount: 1,
		Flags:      0,
		Length:     z.data.Length(),
		BodySize:   0,
	}.WriteTo(w)
	if err != nil {
		return n, err
	}

	nn, err := z.data.WriteTo(w)
	return n + int64(nn), err
}

func readZigzagCodecWithChild[T SignedInteger, U UnsignedInteger](r io.Reader, header Header, childHeader Header) (Codec[T], error) {
	data, err := readCodecWithHeader[U](r, childHeader)
	if err != nil {
		return nil, err
	}
	if header.Length != data.Length() {
		return nil, fmt.Errorf("codec: zigzag length = %d, want %d", header.Length, data.Length())
	}
	return &ZigzagCodec[T, U]{data: data}, nil
}

func readAnyZigzagCodec[T Integer | Float | String](r io.Reader, header Header) (Codec[T], error) {
	var zero T
	switch any(zero).(type) {
	case int8:
		c, err := readZigzagCodec[int8](r, header)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	case int16:
		c, err := readZigzagCodec[int16](r, header)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	case int32:
		c, err := readZigzagCodec[int32](r, header)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	case int64:
		c, err := readZigzagCodec[int64](r, header)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	default:
		return nil, fmt.Errorf("codec: zigzag not supported for type %v", header.ElemType)
	}
}

func readZigzagCodec[T SignedInteger](r io.Reader, header Header) (Codec[T], error) {
	if header.ChildCount != 1 {
		return nil, fmt.Errorf("codec: zigzag child count = %d, want 1", header.ChildCount)
	}
	if header.BodySize != 0 {
		return nil, fmt.Errorf("codec: zigzag body size = %d, want 0", header.BodySize)
	}
	childHeader, err := readHeader(r)
	if err != nil {
		return nil, err
	}
	switch childHeader.ElemType {
	case PTypeUint8:
		return readZigzagCodecWithChild[T, uint8](r, header, childHeader)
	case PTypeUint16:
		return readZigzagCodecWithChild[T, uint16](r, header, childHeader)
	case PTypeUint32:
		return readZigzagCodecWithChild[T, uint32](r, header, childHeader)
	case PTypeUint64:
		return readZigzagCodecWithChild[T, uint64](r, header, childHeader)
	default:
		return nil, fmt.Errorf("codec: zigzag child element type = %v, want unsigned integer", childHeader.ElemType)
	}
}
