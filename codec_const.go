package btrblocks

import (
	"errors"
	"fmt"
	"io"

	"github.com/axiomhq/btrblocks/array"
)

var errValueNotConstant = errors.New("not constant")

// compile-time type assertions
var (
	_ Codec[int8]    = (*ConstCodec[int8])(nil)
	_ Codec[int16]   = (*ConstCodec[int16])(nil)
	_ Codec[int32]   = (*ConstCodec[int32])(nil)
	_ Codec[int64]   = (*ConstCodec[int64])(nil)
	_ Codec[uint8]   = (*ConstCodec[uint8])(nil)
	_ Codec[uint16]  = (*ConstCodec[uint16])(nil)
	_ Codec[uint32]  = (*ConstCodec[uint32])(nil)
	_ Codec[uint64]  = (*ConstCodec[uint64])(nil)
	_ Codec[float32] = (*ConstCodec[float32])(nil)
	_ Codec[float64] = (*ConstCodec[float64])(nil)
	_ Codec[string]  = (*ConstCodec[string])(nil)
)

type ConstCodec[T Integer | Float | String] struct {
	length uint64
	value  T
}

func newConstCodec[T Integer | Float | String](arr array.Array[T], cmpFn cmpFn[T]) (*ConstCodec[T], error) {
	if arr.Length() == 0 {
		return nil, errDataEmpty
	}
	base := arr.ValueAt(0)
	for i := uint64(1); i < arr.Length(); i++ {
		value := arr.ValueAt(i)
		if !cmpFn(base, value) {
			return nil, errValueNotConstant
		}
	}
	return &ConstCodec[T]{length: arr.Length(), value: base}, nil
}

func NewConstIntegerCodec[T Integer](arr array.Array[T]) (*ConstCodec[T], error) {
	return newConstCodec(arr, cmpIntegers[T])
}

func NewConstStringCodec[T String](arr array.Array[T]) (*ConstCodec[T], error) {
	return newConstCodec(arr, cmpStrings[T])
}

func NewConstFloatCodec[T Float](arr array.Array[T]) (*ConstCodec[T], error) {
	return newConstCodec(arr, cmpFloats[T])
}

func (c *ConstCodec[T]) ValueAt(offset uint64) (T, error) {
	if offset >= c.length {
		var zero T
		return zero, errOffsetOutOfRange
	}
	return c.value, nil
}

func (c *ConstCodec[T]) Decode(dst []T) error {
	if err := validateDecodeLength(c.length, len(dst)); err != nil {
		return err
	}
	for i := range dst {
		dst[i] = c.value
	}
	return nil
}

func (c *ConstCodec[T]) BinarySize() uint64 {
	return uint64(headerSize) + constBodyBinarySize(c.value)
}

func (c *ConstCodec[T]) Length() uint64     { return c.length }
func (c *ConstCodec[T]) PType() PType       { return pTypeForType[T]() }
func (c *ConstCodec[T]) Children() []Scheme { return nil }

func (c *ConstCodec[T]) WriteTo(w io.Writer) (int64, error) {
	body := constBodyArray(c.value)
	n, err := Header{
		Version:    1,
		Kind:       CodecTypeConst,
		ElemType:   pTypeForType[T](),
		ChildCount: 0,
		Flags:      0,
		Length:     c.length,
		BodySize:   body.BinarySize(),
	}.WriteTo(w)
	if err != nil {
		return n, err
	}

	nn, err := body.WriteTo(w)
	return n + int64(nn), err
}

// constBodyBinarySize returns the serialized size of a 1-element array without allocating.
func constBodyBinarySize[T Integer | Float | String](value T) uint64 {
	var zero T
	switch any(zero).(type) {
	case string:
		slen := uint64(len(any(value).(string)))
		var offsetWidth uint64
		switch {
		case slen <= ^uint64(uint8(0)):
			offsetWidth = 1
		case slen <= ^uint64(uint16(0)):
			offsetWidth = 2
		default:
			offsetWidth = 4
		}
		return uint64(primitiveArrayHeaderSize) + 4 + 2*offsetWidth + slen
	default:
		return uint64(primitiveArrayHeaderSize) + uint64(pTypeForType[T]().ByteWidth())
	}
}

func constBodyArray[T Integer | Float | String](value T) array.Array[T] {
	var zero T
	switch any(zero).(type) {
	case int8:
		return any(array.NewPrimitivesUnsafe([]int8{any(value).(int8)})).(array.Array[T])
	case int16:
		return any(array.NewPrimitivesUnsafe([]int16{any(value).(int16)})).(array.Array[T])
	case int32:
		return any(array.NewPrimitivesUnsafe([]int32{any(value).(int32)})).(array.Array[T])
	case int64:
		return any(array.NewPrimitivesUnsafe([]int64{any(value).(int64)})).(array.Array[T])
	case uint8:
		return any(array.NewPrimitivesUnsafe([]uint8{any(value).(uint8)})).(array.Array[T])
	case uint16:
		return any(array.NewPrimitivesUnsafe([]uint16{any(value).(uint16)})).(array.Array[T])
	case uint32:
		return any(array.NewPrimitivesUnsafe([]uint32{any(value).(uint32)})).(array.Array[T])
	case uint64:
		return any(array.NewPrimitivesUnsafe([]uint64{any(value).(uint64)})).(array.Array[T])
	case float32:
		return any(array.NewPrimitivesUnsafe([]float32{any(value).(float32)})).(array.Array[T])
	case float64:
		return any(array.NewPrimitivesUnsafe([]float64{any(value).(float64)})).(array.Array[T])
	case string:
		return any(array.NewStrings([]string{any(value).(string)})).(array.Array[T])
	default:
		return nil
	}
}

func readConstCodec[T Integer | Float | String](r io.Reader, header Header) (Codec[T], error) {
	if header.ChildCount != 0 {
		return nil, fmt.Errorf("codec: const child count = %d, want 0", header.ChildCount)
	}
	if header.Length == 0 {
		return nil, fmt.Errorf("codec: const length = 0")
	}
	arr, err := array.ReadArray[T](r)
	if err != nil {
		return nil, err
	}
	if header.BodySize != arr.BinarySize() {
		return nil, fmt.Errorf("codec: const body size = %d, want %d", header.BodySize, arr.BinarySize())
	}
	if arr.Length() != 1 {
		return nil, fmt.Errorf("codec: const body length = %d, want 1", arr.Length())
	}
	return &ConstCodec[T]{length: header.Length, value: arr.ValueAt(0)}, nil
}
