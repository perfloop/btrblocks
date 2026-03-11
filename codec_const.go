package btrblocks

import (
	"errors"
	"io"
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

func newConstCodec[T Integer | Float | String](data []T, cmpFn cmpFn[T]) (*ConstCodec[T], error) {
	if len(data) == 0 {
		return nil, errDataEmpty
	}
	base := data[0]
	for _, value := range data[1:] {
		if !cmpFn(base, value) {
			return nil, errValueNotConstant
		}
	}
	return &ConstCodec[T]{length: uint64(len(data)), value: base}, nil
}

func NewConstIntegerCodec[T Integer](data []T) (*ConstCodec[T], error) {
	return newConstCodec(data, cmpIntegers[T])
}

func NewConstStringCodec[T String](data []T) (*ConstCodec[T], error) {
	return newConstCodec(data, cmpStrings[T])
}

func NewConstFloatCodec[T Float](data []T) (*ConstCodec[T], error) {
	return newConstCodec(data, cmpFloats[T])
}

func (c *ConstCodec[T]) ValueAt(offset uint64) (T, error) {
	if offset >= c.length {
		var zero T
		return zero, errOffsetOutOfRange
	}
	return c.value, nil
}

func (c *ConstCodec[T]) Children() []Scheme                       { return nil }
func (c *ConstCodec[T]) WriteTo(w io.Writer) (n int64, err error) { return int64(c.length), nil }
func (c *ConstCodec[T]) BinarySize() uint64                       { return c.length }
func (c *ConstCodec[T]) Length() uint64                           { return c.length }
func (c *ConstCodec[T]) PType() PType                             { return pTypeForType[T]() }
