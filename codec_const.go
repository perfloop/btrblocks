package btrblocks

import (
	"errors"
	"fmt"
	"io"

	"github.com/axiomhq/btrblocks/array"
)

var errValueNotConstant = errors.New("not constant")

// constArray stores one repeated value for the entire logical array length.
type constArray[T Integer | Float | String] struct {
	length uint64
	value  T
}

func (c *constArray[T]) Encoding() CodeType { return CodecTypeConst }
func (c *constArray[T]) Length() uint64     { return c.length }
func (c *constArray[T]) PType() PType       { return pTypeForType[T]() }
func (c *constArray[T]) BinarySize() uint64 { return uint64(headerSize) + constBodyBinarySize(c.value) }

func (c *constArray[T]) ValueAt(offset uint64) T {
	if offset >= c.length {
		panic(errOffsetOutOfRange)
	}
	return c.value
}

func (c *constArray[T]) Decompress() ([]T, error) {
	dst := make([]T, c.length)
	for i := range dst {
		dst[i] = c.value
	}
	return dst, nil
}

func (c *constArray[T]) Slice(start, end uint64) (EncodedArray[T], error) {
	if err := validateSliceBounds(c.length, start, end); err != nil {
		return nil, err
	}
	return &constArray[T]{length: end - start, value: c.value}, nil
}

func (c *constArray[T]) WriteTo(w io.Writer) (int64, error) {
	body := constBodyArray(c.value)
	n, err := header{
		Version:  versionNumber,
		Kind:     CodecTypeConst,
		ElemType: pTypeForType[T](),
		Length:   c.length,
		BodySize: body.BinarySize(),
	}.WriteTo(w)
	if err != nil {
		return n, err
	}
	nn, err := body.WriteTo(w)
	return n + int64(nn), err
}

func newConstArray[T Integer | Float | String](arr array.Array[T], cmp cmpFn[T]) (*constArray[T], error) {
	if arr.Length() == 0 {
		return nil, errDataEmpty
	}
	value := arr.ValueAt(0)
	for i := uint64(1); i < arr.Length(); i++ {
		if !cmp(value, arr.ValueAt(i)) {
			return nil, errValueNotConstant
		}
	}
	return &constArray[T]{length: arr.Length(), value: value}, nil
}

func newConstIntegerArray[T Integer](arr array.Array[T]) (*constArray[T], error) {
	return newConstArray(arr, cmpIntegers[T])
}

func newConstFloatArray[T Float](arr array.Array[T]) (*constArray[T], error) {
	return newConstArray(arr, cmpFloats[T])
}

func newConstStringArray[T String](arr array.Array[T]) (*constArray[T], error) {
	return newConstArray(arr, cmpStrings[T])
}

func readConstArray[T Integer | Float | String](r io.Reader, h header) (EncodedArray[T], error) {
	arr, err := array.ReadArray[T](r)
	if err != nil {
		return nil, err
	}
	if h.Length == 0 {
		return nil, fmt.Errorf("codec: const length = 0")
	}
	if arr.Length() != 1 {
		return nil, fmt.Errorf("codec: const body length = %d, want 1", arr.Length())
	}
	if h.BodySize != arr.BinarySize() {
		return nil, fmt.Errorf("codec: const body size = %d, want %d", h.BodySize, arr.BinarySize())
	}
	return &constArray[T]{length: h.Length, value: arr.ValueAt(0)}, nil
}

func estimateConst[T Integer | Float | String](isConst bool) func(array.Array[T], planContext) (float64, bool) {
	return func(arr array.Array[T], ctx planContext) (float64, bool) {
		if ctx.isSample || !isConst {
			return 0, false
		}
		return float64(arr.Length()) + 1, true
	}
}

func constBodyBinarySize[T Integer | Float | String](value T) uint64 {
	var zero T
	switch any(zero).(type) {
	case string:
		size := uint64(len(any(value).(string)))
		var offsetWidth uint64
		switch {
		case size <= uint64(^uint8(0)):
			offsetWidth = 1
		case size <= uint64(^uint16(0)):
			offsetWidth = 2
		default:
			offsetWidth = 4
		}
		return uint64(primitiveArrayHeaderSize) + 4 + 2*offsetWidth + size
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
