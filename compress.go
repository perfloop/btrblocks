package btrblocks

import (
	"fmt"
	"io"

	"github.com/axiomhq/btrblocks/array"
)

// Compress encodes arr according to opts. Raw fallback may continue to reference
// arr after Compress returns.
func Compress[T Integer | Float | String](arr array.Array[T], opts Options) (EncodedArray[T], error) {
	return compressArray[T](arr, newPlanContext(opts))
}

func Read[T Integer | Float | String](r io.Reader) (EncodedArray[T], error) {
	return readEncodedArray[T](r)
}

// DecompressInto bulk-decodes encoded into dst. This is the only entry point for
// bulk decompression — EncodedArray itself exposes only scalar ValueAt access.
// Each codec type is matched for an efficient bulk path; unknown types fall back
// to a ValueAt loop.
func DecompressInto[T Integer | Float | String](encoded EncodedArray[T], dst []T) error {
	if encoded.Length() > maxDecompressLength {
		return fmt.Errorf("codec: decompressed length %d exceeds limit %d", encoded.Length(), maxDecompressLength)
	}
	if err := validateCopyLength(encoded.Length(), len(dst)); err != nil {
		return err
	}
	return decompressAny(encoded, dst)
}

// decompressAny dispatches to each codec's decompress method.
func decompressAny[T Integer | Float | String](encoded EncodedArray[T], dst []T) error {
	if d, ok := encoded.(decompressor[T]); ok {
		return d.decompress(dst)
	}
	// Fallback: ValueAt loop for unknown or future codec types.
	for i := range dst {
		dst[i] = encoded.ValueAt(uint64(i))
	}
	return nil
}

// decompressOrdinalsInto bulk-decompresses an ordinalArray into a []uint64 slice.
func decompressOrdinalsInto(ord ordinalArray, dst []uint64) error {
	switch o := ord.(type) {
	case ordinalView[uint8]:
		tmp := make([]uint8, o.codec.Length())
		if err := decompressAny(o.codec, tmp); err != nil {
			return err
		}
		for i, v := range tmp {
			dst[i] = uint64(v)
		}
		return nil
	case ordinalView[uint16]:
		tmp := make([]uint16, o.codec.Length())
		if err := decompressAny(o.codec, tmp); err != nil {
			return err
		}
		for i, v := range tmp {
			dst[i] = uint64(v)
		}
		return nil
	case ordinalView[uint32]:
		tmp := make([]uint32, o.codec.Length())
		if err := decompressAny(o.codec, tmp); err != nil {
			return err
		}
		for i, v := range tmp {
			dst[i] = uint64(v)
		}
		return nil
	case ordinalView[uint64]:
		return decompressAny(o.codec, dst)
	default:
		for i := range dst {
			dst[i] = ord.ValueAt(uint64(i))
		}
		return nil
	}
}

func Decompress[T Integer | Float | String](encoded EncodedArray[T]) ([]T, error) {
	values := make([]T, encoded.Length())
	if err := DecompressInto(encoded, values); err != nil {
		return nil, err
	}
	return values, nil
}

func compressArray[T Integer | Float | String](arr array.Array[T], ctx planContext) (EncodedArray[T], error) {
	var zero T
	switch any(zero).(type) {
	case int8:
		codec, err := compressWith(any(arr).(array.Array[int8]), ctx, signedIntCompressor[int8]{})
		if err != nil {
			return nil, err
		}
		return any(codec).(EncodedArray[T]), nil
	case int16:
		codec, err := compressWith(any(arr).(array.Array[int16]), ctx, signedIntCompressor[int16]{})
		if err != nil {
			return nil, err
		}
		return any(codec).(EncodedArray[T]), nil
	case int32:
		codec, err := compressWith(any(arr).(array.Array[int32]), ctx, signedIntCompressor[int32]{})
		if err != nil {
			return nil, err
		}
		return any(codec).(EncodedArray[T]), nil
	case int64:
		codec, err := compressWith(any(arr).(array.Array[int64]), ctx, signedIntCompressor[int64]{})
		if err != nil {
			return nil, err
		}
		return any(codec).(EncodedArray[T]), nil
	case uint8:
		codec, err := compressWith(any(arr).(array.Array[uint8]), ctx, unsignedIntCompressor[uint8]{})
		if err != nil {
			return nil, err
		}
		return any(codec).(EncodedArray[T]), nil
	case uint16:
		codec, err := compressWith(any(arr).(array.Array[uint16]), ctx, unsignedIntCompressor[uint16]{})
		if err != nil {
			return nil, err
		}
		return any(codec).(EncodedArray[T]), nil
	case uint32:
		codec, err := compressWith(any(arr).(array.Array[uint32]), ctx, unsignedIntCompressor[uint32]{})
		if err != nil {
			return nil, err
		}
		return any(codec).(EncodedArray[T]), nil
	case uint64:
		codec, err := compressWith(any(arr).(array.Array[uint64]), ctx, unsignedIntCompressor[uint64]{})
		if err != nil {
			return nil, err
		}
		return any(codec).(EncodedArray[T]), nil
	case float32:
		codec, err := compressWith(any(arr).(array.Array[float32]), ctx, floatCompressor[float32]{})
		if err != nil {
			return nil, err
		}
		return any(codec).(EncodedArray[T]), nil
	case float64:
		codec, err := compressWith(any(arr).(array.Array[float64]), ctx, floatCompressor[float64]{})
		if err != nil {
			return nil, err
		}
		return any(codec).(EncodedArray[T]), nil
	case string:
		codec, err := compressWith(any(arr).(array.Array[string]), ctx, stringCompressor{})
		if err != nil {
			return nil, err
		}
		return any(codec).(EncodedArray[T]), nil
	default:
		return nil, fmt.Errorf("codec: unsupported element type %T", zero)
	}
}
