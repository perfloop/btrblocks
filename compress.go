package btrblocks

import (
	"fmt"
	"io"

	"github.com/axiomhq/btrblocks/array"
)

const defaultDepth = 3

func compress[T Integer | Float | String](data []T, depth int) Codec[T] {
	var zero T
	switch any(zero).(type) {
	case int8:
		v := any(data).([]int8)
		arr := array.NewPrimitivesUnsafe[int8](v)
		return any(CompressInteger(arr, depth)).(Codec[T])
	case int16:
		v := any(data).([]int16)
		arr := array.NewPrimitivesUnsafe[int16](v)
		return any(CompressInteger(arr, depth)).(Codec[T])
	case int32:
		v := any(data).([]int32)
		arr := array.NewPrimitivesUnsafe[int32](v)
		return any(CompressInteger(arr, depth)).(Codec[T])
	case int64:
		v := any(data).([]int64)
		arr := array.NewPrimitivesUnsafe[int64](v)
		return any(CompressInteger(arr, depth)).(Codec[T])
	case uint8:
		v := any(data).([]uint8)
		arr := array.NewPrimitivesUnsafe[uint8](v)
		return any(CompressInteger(arr, depth)).(Codec[T])
	case uint16:
		v := any(data).([]uint16)
		arr := array.NewPrimitivesUnsafe[uint16](v)
		return any(CompressInteger(arr, depth)).(Codec[T])
	case uint32:
		v := any(data).([]uint32)
		arr := array.NewPrimitivesUnsafe[uint32](v)
		return any(CompressInteger(arr, depth)).(Codec[T])
	case uint64:
		v := any(data).([]uint64)
		arr := array.NewPrimitivesUnsafe[uint64](v)
		return any(CompressInteger(arr, depth)).(Codec[T])
	case float32:
		v := any(data).([]float32)
		arr := array.NewPrimitivesUnsafe[float32](v)
		return any(CompressFloat(arr, depth)).(Codec[T])
	case float64:
		v := any(data).([]float64)
		arr := array.NewPrimitivesUnsafe[float64](v)
		return any(CompressFloat(arr, depth)).(Codec[T])
	case string:
		v := any(data).([]string)
		arr := array.NewStrings(v)
		return any(CompressString(arr, depth)).(Codec[T])
	default:
		return nil
	}
}

func Compress[T Integer | Float | String](data []T) Codec[T] {
	return compress(data, defaultDepth)
}

func CompressWithDepth[T Integer | Float | String](data []T, depth int) Codec[T] {
	return compress(data, depth)
}

func Read[T Integer | Float | String](rdr io.Reader) (Codec[T], error) {
	header, err := readHeader(rdr)
	if err != nil {
		return nil, err
	}
	return readCodecWithHeader[T](rdr, header)
}

// maxDecompressLength is the maximum number of elements Decompress will
// allocate. Callers needing larger outputs should use Read and Decode
// separately with their own allocation strategy.
const maxDecompressLength = 1 << 30 // ~1 billion elements

func Decompress[T Integer | Float | String](rdr io.Reader) ([]T, error) {
	codec, err := readCodec[T](rdr)
	if err != nil {
		return nil, err
	}
	if codec.Length() > maxDecompressLength {
		return nil, fmt.Errorf("codec: decompressed length %d exceeds limit %d; use Read+Decode for large data", codec.Length(), maxDecompressLength)
	}
	data := make([]T, codec.Length())
	if err := codec.Decode(data); err != nil {
		return nil, err
	}
	return data, nil
}
