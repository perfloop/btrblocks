package array

import (
	"fmt"
	"io"
)

// Array is the common interface for columnar array types (primitives and strings).
// All arrays have a fixed PType, support O(1) ValueAt by index, and can be serialized via WriteTo.
type Array[T Integer | Float | String] interface {
	io.WriterTo
	// ValueAt returns the value at the given index. Panics if offset >= Length().
	ValueAt(offset uint64) T
	// BinarySize is the total size in bytes when written (header + body).
	BinarySize() uint64
	// Length is the number of elements in the array.
	Length() uint64
	// PType identifies the element type for this array.
	PType() PType
}

func ReadArray[T Integer | Float | String](r io.Reader) (Array[T], error) {
	header, err := readHeader(r)
	if err != nil {
		return nil, err
	}
	return readArrayWithHeader[T](r, header)
}

func readArrayWithHeader[T Integer | Float | String](r io.Reader, header Header) (Array[T], error) {
	expected := pTypeForType[T]()
	if header.PType != expected {
		return nil, fmt.Errorf("array: PType %v does not match %T", header.PType, *new(T))
	}

	var zero T
	switch any(zero).(type) {
	case int8:
		arr, err := readPrimitivesWithHeader[int8](r, header)
		if err != nil {
			return nil, err
		}
		return any(arr).(Array[T]), nil
	case int16:
		arr, err := readPrimitivesWithHeader[int16](r, header)
		if err != nil {
			return nil, err
		}
		return any(arr).(Array[T]), nil
	case int32:
		arr, err := readPrimitivesWithHeader[int32](r, header)
		if err != nil {
			return nil, err
		}
		return any(arr).(Array[T]), nil
	case int64:
		arr, err := readPrimitivesWithHeader[int64](r, header)
		if err != nil {
			return nil, err
		}
		return any(arr).(Array[T]), nil
	case uint8:
		arr, err := readPrimitivesWithHeader[uint8](r, header)
		if err != nil {
			return nil, err
		}
		return any(arr).(Array[T]), nil
	case uint16:
		arr, err := readPrimitivesWithHeader[uint16](r, header)
		if err != nil {
			return nil, err
		}
		return any(arr).(Array[T]), nil
	case uint32:
		arr, err := readPrimitivesWithHeader[uint32](r, header)
		if err != nil {
			return nil, err
		}
		return any(arr).(Array[T]), nil
	case uint64:
		arr, err := readPrimitivesWithHeader[uint64](r, header)
		if err != nil {
			return nil, err
		}
		return any(arr).(Array[T]), nil
	case float32:
		arr, err := readPrimitivesWithHeader[float32](r, header)
		if err != nil {
			return nil, err
		}
		return any(arr).(Array[T]), nil
	case float64:
		arr, err := readPrimitivesWithHeader[float64](r, header)
		if err != nil {
			return nil, err
		}
		return any(arr).(Array[T]), nil
	case string:
		arr, err := readStringsWithHeader(r, header)
		if err != nil {
			return nil, err
		}
		return any(arr).(Array[T]), nil
	default:
		return nil, fmt.Errorf("array: unsupported element type %T", zero)
	}
}
