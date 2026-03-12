package array

import (
	"fmt"
	"io"
)

var (
	_ Array[int8]    = (*Primitives[int8])(nil)
	_ Array[int16]   = (*Primitives[int16])(nil)
	_ Array[int32]   = (*Primitives[int32])(nil)
	_ Array[int64]   = (*Primitives[int64])(nil)
	_ Array[uint8]   = (*Primitives[uint8])(nil)
	_ Array[uint16]  = (*Primitives[uint16])(nil)
	_ Array[uint32]  = (*Primitives[uint32])(nil)
	_ Array[uint64]  = (*Primitives[uint64])(nil)
	_ Array[float32] = (*Primitives[float32])(nil)
	_ Array[float64] = (*Primitives[float64])(nil)
)

type Primitives[T PrimitiveType] struct {
	pType PType
	data  []T
}

func NewPrimitives[T PrimitiveType](values []T) *Primitives[T] {
	data := make([]T, len(values))
	copy(data, values)
	return NewPrimitivesUnsafe(data)
}

func NewPrimitivesUnsafe[T PrimitiveType](data []T) *Primitives[T] {
	pType := pTypeForType[T]()
	return &Primitives[T]{pType: pType, data: data}
}

func (c *Primitives[T]) ValueAt(offset uint64) T            { return c.data[offset] }
func (c *Primitives[T]) WriteTo(w io.Writer) (int64, error) { return int64(len(c.data)), nil }
func (c *Primitives[T]) BinarySize() uint64                 { return uint64(len(c.data)) * uint64(c.width()) }
func (c *Primitives[T]) Length() uint64                     { return uint64(len(c.data)) }
func (c *Primitives[T]) PType() PType                       { return c.pType }

func (c *Primitives[T]) width() int {
	switch c.pType {
	case PTypeInt8, PTypeUint8:
		return 1
	case PTypeInt16, PTypeUint16:
		return 2
	case PTypeInt32, PTypeUint32:
		return 4
	case PTypeInt64, PTypeUint64:
		return 8
	}
	panic(fmt.Sprintf("unknown primitive type: %v", c.pType))
}
