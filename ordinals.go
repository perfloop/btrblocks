package btrblocks

import (
	"fmt"
	"io"

	"github.com/axiomhq/btrblocks/array"
)

// ordinalArray is the uniform interface for narrowed unsigned index-like children.
type ordinalArray interface {
	io.WriterTo
	BinarySize() uint64
	Length() uint64
	PType() PType
	ValueAt(offset uint64) uint64
	Slice(start, end uint64) (ordinalArray, error)
}

// ordinalView adapts a typed unsigned child array to the ordinalArray interface.
type ordinalView[T UnsignedInteger] struct {
	codec EncodedArray[T]
}

func (o ordinalView[T]) WriteTo(w io.Writer) (int64, error) {
	return o.codec.WriteTo(w)
}

func (o ordinalView[T]) BinarySize() uint64 {
	return o.codec.BinarySize()
}

func (o ordinalView[T]) Length() uint64 {
	return o.codec.Length()
}

func (o ordinalView[T]) PType() PType {
	return o.codec.PType()
}

func (o ordinalView[T]) ValueAt(offset uint64) uint64 {
	return uint64(o.codec.ValueAt(offset))
}

func (o ordinalView[T]) Slice(start, end uint64) (ordinalArray, error) {
	sliced, err := o.codec.Slice(start, end)
	if err != nil {
		return nil, err
	}
	return wrapOrdinalArray(sliced), nil
}

func wrapOrdinalArray[T UnsignedInteger](codec EncodedArray[T]) ordinalArray {
	return ordinalView[T]{codec: codec}
}

func buildOrdinalSlice(maxValue uint64, values []uint64) ordinalArray {
	switch {
	case maxValue <= uint64(^uint8(0)):
		narrow := make([]uint8, len(values))
		for i, value := range values {
			narrow[i] = uint8(value)
		}
		return wrapOrdinalArray(newRawArray(array.NewPrimitivesUnsafe(narrow)))
	case maxValue <= uint64(^uint16(0)):
		narrow := make([]uint16, len(values))
		for i, value := range values {
			narrow[i] = uint16(value)
		}
		return wrapOrdinalArray(newRawArray(array.NewPrimitivesUnsafe(narrow)))
	case maxValue <= uint64(^uint32(0)):
		narrow := make([]uint32, len(values))
		for i, value := range values {
			narrow[i] = uint32(value)
		}
		return wrapOrdinalArray(newRawArray(array.NewPrimitivesUnsafe(narrow)))
	default:
		narrow := make([]uint64, len(values))
		copy(narrow, values)
		return wrapOrdinalArray(newRawArray(array.NewPrimitivesUnsafe(narrow)))
	}
}

func buildCompressedOrdinals(values []uint64, ctx planContext, excludes ...CodeType) (ordinalArray, error) {
	maxValue := uint64(0)
	if len(values) > 0 {
		maxValue = values[len(values)-1]
		for _, value := range values[:len(values)-1] {
			if value > maxValue {
				maxValue = value
			}
		}
	}

	childCtx := ctx.withIntegerExcludes(excludes...)
	switch {
	case maxValue <= uint64(^uint8(0)):
		narrow := make([]uint8, len(values))
		for i, value := range values {
			narrow[i] = uint8(value)
		}
		codec, err := compressArray(array.NewPrimitivesUnsafe(narrow), childCtx)
		if err != nil {
			return nil, err
		}
		return wrapOrdinalArray(codec), nil
	case maxValue <= uint64(^uint16(0)):
		narrow := make([]uint16, len(values))
		for i, value := range values {
			narrow[i] = uint16(value)
		}
		codec, err := compressArray(array.NewPrimitivesUnsafe(narrow), childCtx)
		if err != nil {
			return nil, err
		}
		return wrapOrdinalArray(codec), nil
	case maxValue <= uint64(^uint32(0)):
		narrow := make([]uint32, len(values))
		for i, value := range values {
			narrow[i] = uint32(value)
		}
		codec, err := compressArray(array.NewPrimitivesUnsafe(narrow), childCtx)
		if err != nil {
			return nil, err
		}
		return wrapOrdinalArray(codec), nil
	default:
		narrow := make([]uint64, len(values))
		copy(narrow, values)
		codec, err := compressArray(array.NewPrimitivesUnsafe(narrow), childCtx)
		if err != nil {
			return nil, err
		}
		return wrapOrdinalArray(codec), nil
	}
}

func readOrdinalArray(r io.Reader, h header) (ordinalArray, error) {
	switch h.ElemType {
	case PTypeUint8:
		codec, err := readEncodedArrayWithHeader[uint8](r, h)
		if err != nil {
			return nil, err
		}
		return wrapOrdinalArray(codec), nil
	case PTypeUint16:
		codec, err := readEncodedArrayWithHeader[uint16](r, h)
		if err != nil {
			return nil, err
		}
		return wrapOrdinalArray(codec), nil
	case PTypeUint32:
		codec, err := readEncodedArrayWithHeader[uint32](r, h)
		if err != nil {
			return nil, err
		}
		return wrapOrdinalArray(codec), nil
	case PTypeUint64:
		codec, err := readEncodedArrayWithHeader[uint64](r, h)
		if err != nil {
			return nil, err
		}
		return wrapOrdinalArray(codec), nil
	default:
		return nil, fmt.Errorf("codec: ordinal child type = %v, want unsigned integer", h.ElemType)
	}
}
