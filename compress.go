package btrblocks

import (
	"fmt"
	"io"

	"github.com/axiomhq/btrblocks/array"
)

const defaultDepth = 3

type codecBuilder[T Integer | Float | String] func(array.Array[T], int) (Codec[T], error)

type taggedBuilder[T Integer | Float | String] struct {
	build codecBuilder[T]
	kind  CodecType
}

func selectBest[T Integer | Float | String](arr array.Array[T], depth int, builders []taggedBuilder[T]) Codec[T] {
	if arr.Length() < sampleThreshold {
		return selectBestAll(arr, depth, builders)
	}

	// Stratified sample: evaluate all builders on ~1% of data.
	sample := sampleArray(arr)
	hints := computeSampleHints(sample)

	var (
		bestIdx  = -1
		bestSize uint64
		constIdx = -1
	)
	for i, tb := range builders {
		// Const: skip on samples — false positive risk.
		if tb.kind == CodecTypeConst {
			constIdx = i
			continue
		}
		// Stats-based rejection (Vortex Level 1).
		if hints.shouldSkip(tb.kind) {
			continue
		}
		c, err := tb.build(sample, depth)
		if err != nil {
			continue
		}
		s := c.BinarySize()
		if bestIdx < 0 || s < bestSize {
			bestIdx = i
			bestSize = s
		}
	}

	// Run the sample winner on full data.
	var best Codec[T]
	if bestIdx >= 0 {
		if c, err := builders[bestIdx].build(arr, depth); err == nil {
			best = c
		}
	}

	// Always try Const on full data — it's an O(n) comparison scan with
	// no heavy allocation, and sampling can't reliably detect it.
	if constIdx >= 0 {
		if c, err := builders[constIdx].build(arr, depth); err == nil {
			if best == nil || c.BinarySize() < best.BinarySize() {
				best = c
			}
		}
	}

	if best != nil {
		return best
	}
	// Fallback: Raw never fails.
	return NewRawCodec(arr)
}

// selectBestAll evaluates every builder on arr and returns the smallest codec.
func selectBestAll[T Integer | Float | String](arr array.Array[T], depth int, builders []taggedBuilder[T]) Codec[T] {
	var (
		best     Codec[T]
		bestSize uint64
	)
	for _, tb := range builders {
		if c, err := tb.build(arr, depth); err == nil {
			s := c.BinarySize()
			if best == nil || s < bestSize {
				best = c
				bestSize = s
			}
		}
	}
	return best
}

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
