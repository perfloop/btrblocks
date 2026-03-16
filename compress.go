package btrblocks

import (
	"fmt"
	"io"

	"github.com/axiomhq/btrblocks/array"
)

const defaultDepth = 3

type codecBuilder[T Integer | Float | String] func(array.Array[T], int, codecExcludes) (Codec[T], error)

type taggedBuilder[T Integer | Float | String] struct {
	build codecBuilder[T]
	kind  CodecType
}

func selectBest[T Integer | Float | String](arr array.Array[T], depth int, builders []taggedBuilder[T], excludes codecExcludes) Codec[T] {
	if arr.Length() < sampleThreshold {
		return selectBestAll(arr, depth, builders, excludes)
	}

	hints := computeArrayStats(arr)

	// If stats say the array is constant, build Const and return immediately.
	if hints.isConst && !excludes.has(CodecTypeConst) {
		return &ConstCodec[T]{length: arr.Length(), value: hints.topValue}
	}

	// Trial-compress each candidate on a stratified sample.
	var (
		sample   = sampleArray(arr)
		n        = arr.Length()
		bestIdx  = -1
		bestSize uint64
	)
	for i, tb := range builders {
		if excludes.has(tb.kind) || tb.kind == CodecTypeConst || hints.shouldSkip(tb.kind, n) {
			continue
		}
		c, err := tb.build(sample, depth, excludes)
		if err != nil {
			continue
		}
		if s := c.BinarySize(); bestIdx < 0 || s < bestSize {
			bestIdx = i
			bestSize = s
		}
	}

	// Run the sample winner on full data.
	if bestIdx >= 0 {
		if c, err := builders[bestIdx].build(arr, depth, excludes); err == nil {
			return c
		}
	}
	return NewRawCodec(arr)
}

// selectBestAll evaluates every builder on arr and returns the smallest codec.
func selectBestAll[T Integer | Float | String](arr array.Array[T], depth int, builders []taggedBuilder[T], excludes codecExcludes) Codec[T] {
	var (
		best     Codec[T]
		bestSize uint64
	)
	for _, tb := range builders {
		if excludes.has(tb.kind) {
			continue
		}
		if c, err := tb.build(arr, depth, excludes); err == nil {
			s := c.BinarySize()
			if best == nil || s < bestSize {
				best = c
				bestSize = s
			}
		}
	}
	return best
}

func compress[T Integer | Float | String](data []T, depth int, excludes codecExcludes) Codec[T] {
	var zero T
	switch any(zero).(type) {
	case int8:
		v := any(data).([]int8)
		return any(CompressInteger(array.NewPrimitivesUnsafe(v), depth, excludes)).(Codec[T])
	case int16:
		v := any(data).([]int16)
		return any(CompressInteger(array.NewPrimitivesUnsafe(v), depth, excludes)).(Codec[T])
	case int32:
		v := any(data).([]int32)
		return any(CompressInteger(array.NewPrimitivesUnsafe(v), depth, excludes)).(Codec[T])
	case int64:
		v := any(data).([]int64)
		return any(CompressInteger(array.NewPrimitivesUnsafe(v), depth, excludes)).(Codec[T])
	case uint8:
		v := any(data).([]uint8)
		return any(CompressInteger(array.NewPrimitivesUnsafe(v), depth, excludes)).(Codec[T])
	case uint16:
		v := any(data).([]uint16)
		return any(CompressInteger(array.NewPrimitivesUnsafe(v), depth, excludes)).(Codec[T])
	case uint32:
		v := any(data).([]uint32)
		return any(CompressInteger(array.NewPrimitivesUnsafe(v), depth, excludes)).(Codec[T])
	case uint64:
		v := any(data).([]uint64)
		return any(CompressInteger(array.NewPrimitivesUnsafe(v), depth, excludes)).(Codec[T])
	case float32:
		v := any(data).([]float32)
		return any(CompressFloat(array.NewPrimitivesUnsafe(v), depth, excludes)).(Codec[T])
	case float64:
		v := any(data).([]float64)
		return any(CompressFloat(array.NewPrimitivesUnsafe(v), depth, excludes)).(Codec[T])
	case string:
		v := any(data).([]string)
		return any(CompressString(array.NewStrings(v), depth, excludes)).(Codec[T])
	default:
		return nil
	}
}

func Compress[T Integer | Float | String](data []T) Codec[T] {
	return compress(data, defaultDepth, 0)
}

func CompressWithDepth[T Integer | Float | String](data []T, depth int) Codec[T] {
	return compress(data, depth, 0)
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
