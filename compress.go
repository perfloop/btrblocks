package btrblocks

import (
	"fmt"
	"io"
	"math"
	"sort"

	"github.com/axiomhq/btrblocks/array"
)

type candidate[T Integer | Float | String] struct {
	kind     CodeType
	estimate func(array.Array[T], planContext) (float64, bool)
	build    func(array.Array[T], planContext) (Codec[T], error)
}

func CompressInts[T SignedInteger](values []T, opts Options) (Codec[T], error) {
	return compressSignedArray(array.NewPrimitivesUnsafe(values), newPlanContext(opts))
}

func CompressUints[T UnsignedInteger](values []T, opts Options) (Codec[T], error) {
	return compressUnsignedArray(array.NewPrimitivesUnsafe(values), newPlanContext(opts))
}

func CompressFloats[T Float](values []T, opts Options) (Codec[T], error) {
	return compressFloatArray(array.NewPrimitivesUnsafe(values), newPlanContext(opts))
}

func CompressStrings(values []string, opts Options) (Codec[string], error) {
	return compressStringArray(array.NewStrings(values), newPlanContext(opts))
}

func Read[T Integer | Float | String](r io.Reader) (Codec[T], error) {
	return readCodec[T](r)
}

func rawBinarySize[T Integer | Float | String](arr array.Array[T]) uint64 {
	return newRawCodec(arr).BinarySize()
}

func decompressCodec[T Integer | Float | String](codec Codec[T]) ([]T, error) {
	if codec.Length() > maxDecompressLength {
		return nil, fmt.Errorf("codec: decompressed length %d exceeds limit %d", codec.Length(), maxDecompressLength)
	}
	values := make([]T, codec.Length())
	if err := codec.Decode(values); err != nil {
		return nil, err
	}
	return values, nil
}

func estimateBySample[T Integer | Float | String](arr array.Array[T], ctx planContext, build func(array.Array[T], planContext) (Codec[T], error)) (float64, bool) {
	sampledCtx := ctx.sampled()
	sample := arr
	if !ctx.isSample {
		sample = sampleArray(arr)
	}
	codec, err := build(sample, sampledCtx)
	if err != nil {
		return 0, false
	}
	before := rawBinarySize(sample)
	after := codec.BinarySize()
	if after == 0 {
		return 0, false
	}
	return float64(before) / float64(after), true
}

func selectBest[T Integer | Float | String](arr array.Array[T], ctx planContext, candidates []candidate[T]) (Codec[T], error) {
	raw := newRawCodec(arr)

	type ranked struct {
		idx   int
		ratio float64
	}
	rankedCands := make([]ranked, 0, len(candidates))
	for i, cand := range candidates {
		if ctx.excludes.has(cand.kind) {
			continue
		}
		ratio, ok := cand.estimate(arr, ctx)
		if !ok || ratio <= 1.0 || math.IsNaN(ratio) || math.IsInf(ratio, 0) {
			continue
		}
		rankedCands = append(rankedCands, ranked{idx: i, ratio: ratio})
	}
	sort.SliceStable(rankedCands, func(i, j int) bool {
		return rankedCands[i].ratio > rankedCands[j].ratio
	})

	for _, ranked := range rankedCands {
		codec, err := candidates[ranked.idx].build(arr, ctx)
		if err != nil {
			continue
		}
		if codec.BinarySize() < raw.BinarySize() {
			return codec, nil
		}
	}
	return raw, nil
}

func compressArray[T Integer | Float | String](arr array.Array[T], ctx planContext) (Codec[T], error) {
	var zero T
	switch any(zero).(type) {
	case int8:
		codec, err := compressSignedArray(any(arr).(array.Array[int8]), ctx)
		if err != nil {
			return nil, err
		}
		return any(codec).(Codec[T]), nil
	case int16:
		codec, err := compressSignedArray(any(arr).(array.Array[int16]), ctx)
		if err != nil {
			return nil, err
		}
		return any(codec).(Codec[T]), nil
	case int32:
		codec, err := compressSignedArray(any(arr).(array.Array[int32]), ctx)
		if err != nil {
			return nil, err
		}
		return any(codec).(Codec[T]), nil
	case int64:
		codec, err := compressSignedArray(any(arr).(array.Array[int64]), ctx)
		if err != nil {
			return nil, err
		}
		return any(codec).(Codec[T]), nil
	case uint8:
		codec, err := compressUnsignedArray(any(arr).(array.Array[uint8]), ctx)
		if err != nil {
			return nil, err
		}
		return any(codec).(Codec[T]), nil
	case uint16:
		codec, err := compressUnsignedArray(any(arr).(array.Array[uint16]), ctx)
		if err != nil {
			return nil, err
		}
		return any(codec).(Codec[T]), nil
	case uint32:
		codec, err := compressUnsignedArray(any(arr).(array.Array[uint32]), ctx)
		if err != nil {
			return nil, err
		}
		return any(codec).(Codec[T]), nil
	case uint64:
		codec, err := compressUnsignedArray(any(arr).(array.Array[uint64]), ctx)
		if err != nil {
			return nil, err
		}
		return any(codec).(Codec[T]), nil
	case float32:
		codec, err := compressFloatArray(any(arr).(array.Array[float32]), ctx)
		if err != nil {
			return nil, err
		}
		return any(codec).(Codec[T]), nil
	case float64:
		codec, err := compressFloatArray(any(arr).(array.Array[float64]), ctx)
		if err != nil {
			return nil, err
		}
		return any(codec).(Codec[T]), nil
	case string:
		codec, err := compressStringArray(any(arr).(array.Array[string]), ctx)
		if err != nil {
			return nil, err
		}
		return any(codec).(Codec[T]), nil
	default:
		return nil, fmt.Errorf("codec: unsupported element type %T", zero)
	}
}
