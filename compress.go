package btrblocks

import (
	"fmt"

	"github.com/axiomhq/btrblocks/array"
)

// Compress encodes arr according to opts. Raw fallback may continue to reference
// arr after Compress returns.
func Compress[T Integer | Float | String](arr array.Array[T], opts Options) (EncodedArray[T], error) {
	return compressArray(arr, newPlanContext(opts))
}

// Load deserializes an encoded array from data. The returned EncodedArray may
// reference data's backing memory (zero-copy) — callers must keep data alive
// for the lifetime of the returned value.
func Load[T Integer | Float | String](data []byte, opts ...ReadOptions) (EncodedArray[T], error) {
	var o ReadOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	br := &array.BufReader{Buf: data}
	return readEncodedArray[T](br, o)
}

func compressArray[T Integer | Float | String](arr array.Array[T], ctx planContext) (EncodedArray[T], error) {
	var zero T
	switch any(zero).(type) {
	case int8:
		return readCast[T](compressWith(any(arr).(array.Array[int8]), ctx, &signedIntCompressor[int8]{}))
	case int16:
		return readCast[T](compressWith(any(arr).(array.Array[int16]), ctx, &signedIntCompressor[int16]{}))
	case int32:
		return readCast[T](compressWith(any(arr).(array.Array[int32]), ctx, &signedIntCompressor[int32]{}))
	case int64:
		return readCast[T](compressWith(any(arr).(array.Array[int64]), ctx, &signedIntCompressor[int64]{}))
	case uint8:
		return readCast[T](compressWith(any(arr).(array.Array[uint8]), ctx, &unsignedIntCompressor[uint8]{}))
	case uint16:
		return readCast[T](compressWith(any(arr).(array.Array[uint16]), ctx, &unsignedIntCompressor[uint16]{}))
	case uint32:
		return readCast[T](compressWith(any(arr).(array.Array[uint32]), ctx, &unsignedIntCompressor[uint32]{}))
	case uint64:
		return readCast[T](compressWith(any(arr).(array.Array[uint64]), ctx, &unsignedIntCompressor[uint64]{}))
	case float32:
		return readCast[T](compressWith(any(arr).(array.Array[float32]), ctx, &floatCompressor[float32]{}))
	case float64:
		return readCast[T](compressWith(any(arr).(array.Array[float64]), ctx, &floatCompressor[float64]{}))
	case string:
		return readCast[T](compressWith(any(arr).(array.Array[string]), ctx, stringCompressor{}))
	default:
		return nil, fmt.Errorf("codec: unsupported element type %T", zero)
	}
}
