package btrblocks

import (
	"io"

	"github.com/axiomhq/btrblocks/array"
)

var (
	_ Codec[int8]   = (*ZigzagCodec[int8])(nil)
	_ Codec[int16]  = (*ZigzagCodec[int16])(nil)
	_ Codec[int32]  = (*ZigzagCodec[int32])(nil)
	_ Codec[int64]  = (*ZigzagCodec[int64])(nil)
	_ Codec[uint8]  = (*ZigzagCodec[uint8])(nil)
	_ Codec[uint16] = (*ZigzagCodec[uint16])(nil)
	_ Codec[uint32] = (*ZigzagCodec[uint32])(nil)
	_ Codec[uint64] = (*ZigzagCodec[uint64])(nil)
)

type ZigzagCodec[T Integer] struct {
	data Codec[uint64]
}

func zigzagEncode64(n int64) uint64 {
	return uint64((n << 1) ^ (n >> 63))
}

func zigzagDecode64(z uint64) int64 {
	return int64(z>>1) ^ -int64(z&1)
}

func zigzagEncodeSlice[T Integer](vals []T) []uint64 {
	out := make([]uint64, len(vals))
	switch any(*new(T)).(type) {
	case int8:
		for i, v := range vals {
			out[i] = zigzagEncode64(int64(any(v).(int8)))
		}
	case int16:
		for i, v := range vals {
			out[i] = zigzagEncode64(int64(any(v).(int16)))
		}
	case int32:
		for i, v := range vals {
			out[i] = zigzagEncode64(int64(any(v).(int32)))
		}
	case int64:
		for i, v := range vals {
			out[i] = zigzagEncode64(any(v).(int64))
		}
	case uint8:
		for i, v := range vals {
			out[i] = uint64(any(v).(uint8))
		}
	case uint16:
		for i, v := range vals {
			out[i] = uint64(any(v).(uint16))
		}
	case uint32:
		for i, v := range vals {
			out[i] = uint64(any(v).(uint32))
		}
	case uint64:
		for i, v := range vals {
			out[i] = any(v).(uint64)
		}
	default:
		return out
	}
	return out
}

func NewZigzagCodec[T Integer](vals []T, depth int) (*ZigzagCodec[T], error) {
	if depth <= 0 {
		return nil, errDepthExhausted
	}
	encoded := zigzagEncodeSlice(vals)
	inner := CompressInteger(array.NewPrimitivesUnsafe(encoded), depth-1)
	return &ZigzagCodec[T]{data: inner}, nil
}

func (z *ZigzagCodec[T]) ValueAt(offset uint64) (T, error) {
	var zero T
	u, err := z.data.ValueAt(offset)
	if err != nil {
		return zero, err
	}
	switch any(zero).(type) {
	case int8:
		return any(int8(zigzagDecode64(u))).(T), nil
	case int16:
		return any(int16(zigzagDecode64(u))).(T), nil
	case int32:
		return any(int32(zigzagDecode64(u))).(T), nil
	case int64:
		return any(zigzagDecode64(u)).(T), nil
	case uint8:
		return any(uint8(u)).(T), nil
	case uint16:
		return any(uint16(u)).(T), nil
	case uint32:
		return any(uint32(u)).(T), nil
	case uint64:
		return any(u).(T), nil
	default:
		return zero, nil
	}
}

func (z *ZigzagCodec[T]) Children() []Scheme {
	return []Scheme{z.data}
}

func (z *ZigzagCodec[T]) BinarySize() uint64 { return uint64(headerSize) + z.data.BinarySize() }
func (z *ZigzagCodec[T]) Length() uint64     { return z.data.Length() }
func (z *ZigzagCodec[T]) PType() PType       { return pTypeForType[T]() }

func (z *ZigzagCodec[T]) WriteTo(w io.Writer) (n int64, err error) {
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
