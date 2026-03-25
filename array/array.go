package array

import (
	"fmt"
	"io"
	"unsafe"
)

func init() {
	var x uint32 = 0x01020304
	if *(*byte)(unsafe.Pointer(&x)) != 0x04 {
		panic("array: unsafe I/O requires a little-endian platform")
	}
}

// ArrayCore is the minimal read interface for columnar data: element access and length.
// Used by planner estimation and codec build paths that only need to scan values.
type ArrayCore[T Integer | Float | String] interface {
	// ValueAt returns the value at the given index. Panics if offset >= Length().
	ValueAt(offset uint64) T
	// Length is the number of elements in the array.
	Length() uint64
}

// Array is a fully materialized columnar array that can be serialized and sliced.
type Array[T Integer | Float | String] interface {
	ArrayCore[T]
	io.WriterTo
	// CopyTo copies all elements into dst. len(dst) must be >= Length().
	CopyTo(dst []T)
	// Slice returns a view of the half-open interval [start, end).
	Slice(start, end uint64) (Array[T], error)
	// BinarySize is the total size in bytes when written (header + body).
	BinarySize() uint64
	// PType identifies the element type for this array.
	PType() PType
}

func ValidateSliceBounds(length, start, end uint64) error {
	if start > end {
		return fmt.Errorf("array: slice start = %d, want <= %d", start, end)
	}
	if end > length {
		return fmt.Errorf("array: slice end = %d, want <= %d", end, length)
	}
	return nil
}

func ReadArray[T Integer | Float | String](r io.Reader, opts ...ReadOptions) (Array[T], error) {
	header, err := readHeader(r)
	if err != nil {
		return nil, err
	}
	return readArrayWithHeader[T](r, header, readOpts(opts))
}

func readOpts(opts []ReadOptions) ReadOptions {
	if len(opts) > 0 {
		return opts[0]
	}
	return ReadOptions{}
}

func readArrayWithHeader[T Integer | Float | String](r io.Reader, header Header, opts ReadOptions) (Array[T], error) {
	if err := validateHeader(header, opts); err != nil {
		return nil, err
	}
	expected := PTypeForType[T]()
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
