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

func decompressAny[T Integer | Float | String](encoded EncodedArray[T], dst []T) error {
	switch c := any(encoded).(type) {
	// --- leaf codecs (no children to recurse into) ---
	case *rawArray[T]:
		c.arr.CopyTo(dst)
		return nil
	case *constArray[T]:
		for i := range dst {
			dst[i] = c.value
		}
		return nil
	case *sequenceArray[int8]:
		return decompressSequence(c, any(dst).([]int8))
	case *sequenceArray[int16]:
		return decompressSequence(c, any(dst).([]int16))
	case *sequenceArray[int32]:
		return decompressSequence(c, any(dst).([]int32))
	case *sequenceArray[int64]:
		return decompressSequence(c, any(dst).([]int64))
	case *sequenceArray[uint8]:
		return decompressSequence(c, any(dst).([]uint8))
	case *sequenceArray[uint16]:
		return decompressSequence(c, any(dst).([]uint16))
	case *sequenceArray[uint32]:
		return decompressSequence(c, any(dst).([]uint32))
	case *sequenceArray[uint64]:
		return decompressSequence(c, any(dst).([]uint64))

	// --- bitpack ---
	case *bitPackedArray[uint8]:
		return decompressBitPacked(c, any(dst).([]uint8))
	case *bitPackedArray[uint16]:
		return decompressBitPacked(c, any(dst).([]uint16))
	case *bitPackedArray[uint32]:
		return decompressBitPacked(c, any(dst).([]uint32))
	case *bitPackedArray[uint64]:
		return decompressBitPacked(c, any(dst).([]uint64))

	// --- FoR ---
	case *forArray[uint8]:
		return decompressFoR(c, any(dst).([]uint8))
	case *forArray[uint16]:
		return decompressFoR(c, any(dst).([]uint16))
	case *forArray[uint32]:
		return decompressFoR(c, any(dst).([]uint32))
	case *forArray[uint64]:
		return decompressFoR(c, any(dst).([]uint64))

	// --- zigzag ---
	case *zigzagArray[int8, uint8]:
		return decompressZigZag[int8, uint8](c, any(dst).([]int8))
	case *zigzagArray[int8, uint16]:
		return decompressZigZag[int8, uint16](c, any(dst).([]int8))
	case *zigzagArray[int8, uint32]:
		return decompressZigZag[int8, uint32](c, any(dst).([]int8))
	case *zigzagArray[int8, uint64]:
		return decompressZigZag[int8, uint64](c, any(dst).([]int8))
	case *zigzagArray[int16, uint8]:
		return decompressZigZag[int16, uint8](c, any(dst).([]int16))
	case *zigzagArray[int16, uint16]:
		return decompressZigZag[int16, uint16](c, any(dst).([]int16))
	case *zigzagArray[int16, uint32]:
		return decompressZigZag[int16, uint32](c, any(dst).([]int16))
	case *zigzagArray[int16, uint64]:
		return decompressZigZag[int16, uint64](c, any(dst).([]int16))
	case *zigzagArray[int32, uint8]:
		return decompressZigZag[int32, uint8](c, any(dst).([]int32))
	case *zigzagArray[int32, uint16]:
		return decompressZigZag[int32, uint16](c, any(dst).([]int32))
	case *zigzagArray[int32, uint32]:
		return decompressZigZag[int32, uint32](c, any(dst).([]int32))
	case *zigzagArray[int32, uint64]:
		return decompressZigZag[int32, uint64](c, any(dst).([]int32))
	case *zigzagArray[int64, uint8]:
		return decompressZigZag[int64, uint8](c, any(dst).([]int64))
	case *zigzagArray[int64, uint16]:
		return decompressZigZag[int64, uint16](c, any(dst).([]int64))
	case *zigzagArray[int64, uint32]:
		return decompressZigZag[int64, uint32](c, any(dst).([]int64))
	case *zigzagArray[int64, uint64]:
		return decompressZigZag[int64, uint64](c, any(dst).([]int64))

	// --- dict ---
	case *dictArray[T]:
		return decompressDict(c, dst)

	// --- runend ---
	case *runEndArray[T]:
		return decompressRunEnd(c, dst)

	// --- ALP ---
	case *alpArray64:
		return decompressALP64(c, any(dst).([]float64))
	case *alpArray32:
		return decompressALP32(c, any(dst).([]float32))

	default:
		// Fallback: ValueAt loop for unknown or future codec types.
		for i := range dst {
			dst[i] = encoded.ValueAt(uint64(i))
		}
		return nil
	}
}

func decompressSequence[T Integer](c *sequenceArray[T], dst []T) error {
	for i := range dst {
		dst[i] = c.base + T(i)*c.step
	}
	return nil
}

func decompressBitPacked[T UnsignedInteger](c *bitPackedArray[T], dst []T) error {
	if c.bitWidth == 0 {
		var zero T
		for i := range dst {
			dst[i] = zero
		}
		return nil
	}
	for i := range dst {
		dst[i] = T(unpackUnsigned(c.buf, uint64(i)*uint64(c.bitWidth), c.bitWidth))
	}
	return c.patches.Apply(dst)
}

func decompressFoR[T UnsignedInteger](c *forArray[T], dst []T) error {
	if err := decompressAny(c.child, dst); err != nil {
		return err
	}
	for i := range dst {
		dst[i] += c.min
	}
	return nil
}

func decompressZigZag[T SignedInteger, U UnsignedInteger](c *zigzagArray[T, U], dst []T) error {
	encoded := make([]U, c.child.Length())
	if err := decompressAny(c.child, encoded); err != nil {
		return err
	}
	for i, value := range encoded {
		dst[i] = T(zigzagDecode64(uint64(value)))
	}
	return nil
}

func decompressDict[T Integer | Float | String](c *dictArray[T], dst []T) error {
	// Rust pattern: execute both children to flat arrays, then gather.
	values := make([]T, c.values.Length())
	if err := decompressAny(c.values, values); err != nil {
		return err
	}
	indices := make([]uint64, c.indices.Length())
	if err := decompressOrdinalsInto(c.indices, indices); err != nil {
		return err
	}
	for i, idx := range indices {
		dst[i] = values[idx]
	}
	return nil
}

func decompressRunEnd[T Integer | Float | String](c *runEndArray[T], dst []T) error {
	// Rust pattern: execute both children to flat arrays, then expand runs.
	runs := make([]T, c.runs.Length())
	if err := decompressAny(c.runs, runs); err != nil {
		return err
	}
	ends := make([]uint64, c.ends.Length())
	if err := decompressOrdinalsInto(c.ends, ends); err != nil {
		return err
	}
	pos := 0
	for i, rawEnd := range ends {
		end := int(rawEnd)
		fillRun(dst, pos, end, runs[i])
		pos = end
	}
	fillRun(dst, pos, len(dst), runs[len(ends)])
	return nil
}

// decompressOrdinalsInto bulk-decompresses an ordinalArray into a []uint64 slice
// by unwrapping the typed EncodedArray child and calling decompressAny on it.
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

func decompressALP64(c *alpArray64, dst []float64) error {
	encoded := make([]int64, c.length)
	if err := decompressAny(c.encoded, encoded); err != nil {
		return err
	}
	for i, value := range encoded {
		dst[i] = alpDecode64(value, c.expE, c.expF)
	}
	return c.patches.Apply(dst)
}

func decompressALP32(c *alpArray32, dst []float32) error {
	encoded := make([]int32, c.length)
	if err := decompressAny(c.encoded, encoded); err != nil {
		return err
	}
	for i, value := range encoded {
		dst[i] = alpDecode32(value, c.expE, c.expF)
	}
	return c.patches.Apply(dst)
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
