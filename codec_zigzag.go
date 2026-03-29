package btrblocks

import (
	"fmt"
	"io"

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
	return array.HeaderSize + a.length*uint64(array.PTypeForType[U]().ByteWidth())
}

func (a zigzagEncodedArray[T, U]) Length() uint64 { return a.length }
func (a zigzagEncodedArray[T, U]) PType() PType   { return array.PTypeForType[U]() }

func (a zigzagEncodedArray[T, U]) Slice(start, end uint64) (array.Array[U], error) {
	return materializeSlice(a, start, end)
}

func (a zigzagEncodedArray[T, U]) WriteTo(w io.Writer) (int64, error) {
	return writeVirtualArray(w, a.length, func(i uint64) U {
		return U(zigzagEncodeValue(a.valueAt(i)))
	})
}

func zigzagEncode64(n int64) uint64                     { return uint64((n << 1) ^ (n >> 63)) }
func zigzagDecode64(z uint64) int64                     { return int64(z>>1) ^ -int64(z&1) }
func zigzagEncodeValue[T SignedInteger](value T) uint64 { return zigzagEncode64(int64(value)) }

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
func (z *zigzagArray[T, U]) PType() PType       { return array.PTypeForType[T]() }
func (z *zigzagArray[T, U]) BinarySize() uint64 { return uint64(headerSize) + z.child.BinarySize() }

func (z *zigzagArray[T, U]) ValueAt(offset uint64) T {
	value := uint64(z.child.ValueAt(offset))
	return T(zigzagDecode64(value))
}

func (z *zigzagArray[T, U]) DecompressInto(dst []T) error {
	if err := checkDstLen(dst, z.child.Length()); err != nil {
		return err
	}
	encoded, err := Decompress(z.child)
	if err != nil {
		return err
	}
	for i, value := range encoded {
		dst[i] = T(zigzagDecode64(uint64(value)))
	}
	return nil
}

func (z *zigzagArray[T, U]) Slice(start, end uint64) (EncodedArray[T], error) {
	child, err := z.child.Slice(start, end)
	if err != nil {
		return nil, err
	}
	return &zigzagArray[T, U]{child: child}, nil
}

func (z *zigzagArray[T, U]) WriteTo(w io.Writer) (int64, error) {
	n, err := codecHeader{
		Version:  versionNumber,
		Kind:     CodecTypeZigZag,
		ElemType: array.PTypeForType[T](),
		Length:   z.child.Length(),
		NumBytes: 0,
	}.WriteTo(w)
	if err != nil {
		return n, err
	}
	nn, err := z.child.WriteTo(w)
	return n + nn, err
}

func readAnyZigZagArray[T Integer | Float | String](br *array.BufReader, h codecHeader, opts ReadOptions) (EncodedArray[T], error) {
	var zero T
	switch any(zero).(type) {
	case int8:
		return readCast[T](readZigZagArray[int8](br, h, opts))
	case int16:
		return readCast[T](readZigZagArray[int16](br, h, opts))
	case int32:
		return readCast[T](readZigZagArray[int32](br, h, opts))
	case int64:
		return readCast[T](readZigZagArray[int64](br, h, opts))
	default:
		return nil, fmt.Errorf("codec: zigzag not supported for %v", h.ElemType)
	}
}

func readZigZagArray[T SignedInteger](br *array.BufReader, h codecHeader, opts ReadOptions) (EncodedArray[T], error) {
	if h.NumBytes != 0 {
		return nil, fmt.Errorf("codec: zigzag body size = %d, want 0", h.NumBytes)
	}
	childHeader, err := readHeader(br)
	if err != nil {
		return nil, err
	}
	switch childHeader.ElemType {
	case PTypeUint8:
		return readZigZagChild[T, uint8](br, h, childHeader, opts)
	case PTypeUint16:
		return readZigZagChild[T, uint16](br, h, childHeader, opts)
	case PTypeUint32:
		return readZigZagChild[T, uint32](br, h, childHeader, opts)
	case PTypeUint64:
		return readZigZagChild[T, uint64](br, h, childHeader, opts)
	default:
		return nil, fmt.Errorf("codec: zigzag child type = %v, want unsigned integer", childHeader.ElemType)
	}
}

func readZigZagChild[T SignedInteger, U UnsignedInteger](br *array.BufReader, h codecHeader, childHeader codecHeader, opts ReadOptions) (EncodedArray[T], error) {
	child, err := readEncodedArrayWithHeader[U](br, childHeader, opts)
	if err != nil {
		return nil, err
	}
	if child.Length() != h.Length {
		return nil, fmt.Errorf("codec: zigzag length = %d, want %d", child.Length(), h.Length)
	}
	return &zigzagArray[T, U]{child: child}, nil
}

func buildZigZagArray[T SignedInteger](arr array.ArrayCore[T], ctx planContext) (EncodedArray[T], error) {
	if ctx.depth <= 0 {
		return nil, errDepthExhausted
	}
	max := zigzagMaxEncoded(arr.Length(), arr.ValueAt)
	switch {
	case max <= uint64(^uint8(0)):
		return buildZigZagWithWidth[T, uint8](arr, ctx)
	case max <= uint64(^uint16(0)):
		return buildZigZagWithWidth[T, uint16](arr, ctx)
	case max <= uint64(^uint32(0)):
		return buildZigZagWithWidth[T, uint32](arr, ctx)
	default:
		return buildZigZagWithWidth[T, uint64](arr, ctx)
	}
}

func buildZigZagWithWidth[T SignedInteger, U UnsignedInteger](arr array.ArrayCore[T], ctx planContext) (EncodedArray[T], error) {
	child, err := compressArray(zigzagEncodedArray[T, U]{length: arr.Length(), valueAt: arr.ValueAt}, ctx.descend().withIntegerExcludes(CodecTypeZigZag, CodecTypeDict, CodecTypeRunEnd))
	if err != nil {
		return nil, err
	}
	return &zigzagArray[T, U]{child: child}, nil
}

func estimateZigZag[T SignedInteger, S statsSource[T]](stats S, ctx planContext, hasNegative bool) (float64, bool) {
	if ctx.depth <= 0 || !hasNegative {
		return 0, false
	}
	return estimateBySample(stats, ctx, buildZigZagArray[T])
}
