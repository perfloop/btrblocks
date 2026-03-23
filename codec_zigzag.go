package btrblocks

import (
	"fmt"
	"io"
	"unsafe"

	"github.com/axiomhq/btrblocks/array"
)

// zigzagArray stores signed integers by delegating to an unsigned child array.
type zigzagArray[T SignedInteger, U UnsignedInteger] struct {
	child EncodedArray[U]
}

// zigzagEncodedArray presents zigzag-transformed values as an unsigned array view.
type zigzagEncodedArray[T SignedInteger, U UnsignedInteger] struct {
	length  uint64
	valueAt func(uint64) T
}

func (a zigzagEncodedArray[T, U]) ValueAt(offset uint64) U {
	return U(zigzagEncodeValue(a.valueAt(offset)))
}

func (a zigzagEncodedArray[T, U]) CopyTo(dst []U) {
	for i := range dst {
		dst[i] = U(zigzagEncodeValue(a.valueAt(uint64(i))))
	}
}

func (a zigzagEncodedArray[T, U]) BinarySize() uint64 {
	return primitiveArrayHeaderSize + a.length*uint64(pTypeForType[U]().ByteWidth())
}

func (a zigzagEncodedArray[T, U]) Length() uint64 { return a.length }
func (a zigzagEncodedArray[T, U]) PType() PType   { return pTypeForType[U]() }

func (a zigzagEncodedArray[T, U]) Slice(start, end uint64) (array.Array[U], error) {
	return materializeSlice[U](a, start, end)
}

func (a zigzagEncodedArray[T, U]) WriteTo(w io.Writer) (int64, error) {
	bodySize := a.length * uint64(pTypeForType[U]().ByteWidth())
	n, err := array.Header{
		Version:  versionNumber,
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
		if remaining := a.length - offset; remaining < uint64(chunk) {
			chunk = int(remaining)
		}
		for i := 0; i < chunk; i++ {
			buf[i] = U(zigzagEncodeValue(a.valueAt(offset + uint64(i))))
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

func zigzagEncodeValue[T SignedInteger](value T) uint64 {
	switch v := any(value).(type) {
	case int8:
		return zigzagEncode64(int64(v))
	case int16:
		return zigzagEncode64(int64(v))
	case int32:
		return zigzagEncode64(int64(v))
	case int64:
		return zigzagEncode64(v)
	default:
		return 0
	}
}

func zigzagMaxEncoded[T SignedInteger](length uint64, valueAt func(uint64) T) uint64 {
	var max uint64
	for i := uint64(0); i < length; i++ {
		if encoded := zigzagEncodeValue(valueAt(i)); encoded > max {
			max = encoded
		}
	}
	return max
}

func (z *zigzagArray[T, U]) Encoding() CodeType { return CodecTypeZigZag }
func (z *zigzagArray[T, U]) Length() uint64     { return z.child.Length() }
func (z *zigzagArray[T, U]) PType() PType       { return pTypeForType[T]() }
func (z *zigzagArray[T, U]) BinarySize() uint64 { return uint64(headerSize) + z.child.BinarySize() }

func (z *zigzagArray[T, U]) ValueAt(offset uint64) T {
	value := uint64(z.child.ValueAt(offset))
	var zero T
	switch any(zero).(type) {
	case int8:
		return any(int8(zigzagDecode64(value))).(T)
	case int16:
		return any(int16(zigzagDecode64(value))).(T)
	case int32:
		return any(int32(zigzagDecode64(value))).(T)
	case int64:
		return any(zigzagDecode64(value)).(T)
	default:
		panic(fmt.Errorf("codec: zigzag not supported for %T", zero))
	}
}

func (z *zigzagArray[T, U]) Slice(start, end uint64) (EncodedArray[T], error) {
	child, err := z.child.Slice(start, end)
	if err != nil {
		return nil, err
	}
	return &zigzagArray[T, U]{child: child}, nil
}

func (z *zigzagArray[T, U]) WriteTo(w io.Writer) (int64, error) {
	n, err := header{
		Version:  versionNumber,
		Kind:     CodecTypeZigZag,
		ElemType: pTypeForType[T](),
		Length:   z.child.Length(),
		BodySize: 0,
	}.WriteTo(w)
	if err != nil {
		return n, err
	}
	nn, err := z.child.WriteTo(w)
	return n + nn, err
}

func readAnyZigZagArray[T Integer | Float | String](r io.Reader, h header) (EncodedArray[T], error) {
	var zero T
	switch any(zero).(type) {
	case int8:
		c, err := readZigZagArray[int8](r, h)
		if err != nil {
			return nil, err
		}
		return any(c).(EncodedArray[T]), nil
	case int16:
		c, err := readZigZagArray[int16](r, h)
		if err != nil {
			return nil, err
		}
		return any(c).(EncodedArray[T]), nil
	case int32:
		c, err := readZigZagArray[int32](r, h)
		if err != nil {
			return nil, err
		}
		return any(c).(EncodedArray[T]), nil
	case int64:
		c, err := readZigZagArray[int64](r, h)
		if err != nil {
			return nil, err
		}
		return any(c).(EncodedArray[T]), nil
	default:
		return nil, fmt.Errorf("codec: zigzag not supported for %v", h.ElemType)
	}
}

func readZigZagArray[T SignedInteger](r io.Reader, h header) (EncodedArray[T], error) {
	if h.BodySize != 0 {
		return nil, fmt.Errorf("codec: zigzag body size = %d, want 0", h.BodySize)
	}
	childHeader, err := readHeader(r)
	if err != nil {
		return nil, err
	}
	switch childHeader.ElemType {
	case PTypeUint8:
		child, err := readEncodedArrayWithHeader[uint8](r, childHeader)
		if err != nil {
			return nil, err
		}
		if child.Length() != h.Length {
			return nil, fmt.Errorf("codec: zigzag length = %d, want %d", h.Length, child.Length())
		}
		return &zigzagArray[T, uint8]{child: child}, nil
	case PTypeUint16:
		child, err := readEncodedArrayWithHeader[uint16](r, childHeader)
		if err != nil {
			return nil, err
		}
		if child.Length() != h.Length {
			return nil, fmt.Errorf("codec: zigzag length = %d, want %d", h.Length, child.Length())
		}
		return &zigzagArray[T, uint16]{child: child}, nil
	case PTypeUint32:
		child, err := readEncodedArrayWithHeader[uint32](r, childHeader)
		if err != nil {
			return nil, err
		}
		if child.Length() != h.Length {
			return nil, fmt.Errorf("codec: zigzag length = %d, want %d", h.Length, child.Length())
		}
		return &zigzagArray[T, uint32]{child: child}, nil
	case PTypeUint64:
		child, err := readEncodedArrayWithHeader[uint64](r, childHeader)
		if err != nil {
			return nil, err
		}
		if child.Length() != h.Length {
			return nil, fmt.Errorf("codec: zigzag length = %d, want %d", h.Length, child.Length())
		}
		return &zigzagArray[T, uint64]{child: child}, nil
	default:
		return nil, fmt.Errorf("codec: zigzag child type = %v, want unsigned integer", childHeader.ElemType)
	}
}

func buildZigZagArray[T SignedInteger](arr array.Array[T], ctx planContext) (EncodedArray[T], error) {
	if ctx.depth <= 0 {
		return nil, errDepthExhausted
	}
	max := zigzagMaxEncoded(arr.Length(), arr.ValueAt)
	switch {
	case max <= uint64(^uint8(0)):
		child, err := compressArray[uint8](zigzagEncodedArray[T, uint8]{length: arr.Length(), valueAt: arr.ValueAt}, ctx.descend().withIntegerExcludes(CodecTypeZigZag, CodecTypeDict, CodecTypeRunEnd))
		if err != nil {
			return nil, err
		}
		return &zigzagArray[T, uint8]{child: child}, nil
	case max <= uint64(^uint16(0)):
		child, err := compressArray[uint16](zigzagEncodedArray[T, uint16]{length: arr.Length(), valueAt: arr.ValueAt}, ctx.descend().withIntegerExcludes(CodecTypeZigZag, CodecTypeDict, CodecTypeRunEnd))
		if err != nil {
			return nil, err
		}
		return &zigzagArray[T, uint16]{child: child}, nil
	case max <= uint64(^uint32(0)):
		child, err := compressArray[uint32](zigzagEncodedArray[T, uint32]{length: arr.Length(), valueAt: arr.ValueAt}, ctx.descend().withIntegerExcludes(CodecTypeZigZag, CodecTypeDict, CodecTypeRunEnd))
		if err != nil {
			return nil, err
		}
		return &zigzagArray[T, uint32]{child: child}, nil
	default:
		child, err := compressArray[uint64](zigzagEncodedArray[T, uint64]{length: arr.Length(), valueAt: arr.ValueAt}, ctx.descend().withIntegerExcludes(CodecTypeZigZag, CodecTypeDict, CodecTypeRunEnd))
		if err != nil {
			return nil, err
		}
		return &zigzagArray[T, uint64]{child: child}, nil
	}
}

func estimateZigZag[T SignedInteger, S statsSource[T]](hasNegative bool) func(S, planContext) (float64, bool) {
	return func(stats S, ctx planContext) (float64, bool) {
		if ctx.depth <= 0 || !hasNegative {
			return 0, false
		}
		return estimateBySample(stats, ctx, buildZigZagArray[T])
	}
}
