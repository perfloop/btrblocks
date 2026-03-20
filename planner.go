package btrblocks

import (
	"math"

	"github.com/axiomhq/btrblocks/array"
)

type statsSource[T Integer | Float | String] interface {
	Source() array.Array[T]
	Sample(planContext) array.Array[T]
}

type scheme[T Integer | Float | String, S statsSource[T]] interface {
	Kind() CodeType
	Estimate(stats S, ctx planContext) (float64, bool)
	Build(arr array.Array[T], ctx planContext) (Codec[T], error)
}

type registeredScheme[T Integer | Float | String, S statsSource[T]] struct {
	kind     CodeType
	estimate func(stats S, ctx planContext) (float64, bool)
	build    func(arr array.Array[T], ctx planContext) (Codec[T], error)
}

func (s registeredScheme[T, S]) Kind() CodeType {
	return s.kind
}

func (s registeredScheme[T, S]) Estimate(stats S, ctx planContext) (float64, bool) {
	if s.estimate == nil {
		return 0, false
	}
	return s.estimate(stats, ctx)
}

func (s registeredScheme[T, S]) Build(arr array.Array[T], ctx planContext) (Codec[T], error) {
	return s.build(arr, ctx)
}

type compressor[T Integer | Float | String, S statsSource[T]] interface {
	ComputeStats(arr array.Array[T]) S
	Schemes() []scheme[T, S]
	IsExcluded(ctx planContext, kind CodeType) bool
}

func compressWith[T Integer | Float | String, S statsSource[T]](arr array.Array[T], ctx planContext, c compressor[T, S]) (Codec[T], error) {
	stats := c.ComputeStats(arr)
	raw := Codec[T](newRawCodec(arr))

	scheme, ok := chooseScheme(stats, ctx, c)
	if !ok {
		return raw, nil
	}

	codec, err := scheme.Build(arr, ctx)
	if err != nil {
		return raw, nil
	}
	if codec.BinarySize() >= raw.BinarySize() {
		return raw, nil
	}
	return codec, nil
}

func chooseScheme[T Integer | Float | String, S statsSource[T]](stats S, ctx planContext, c compressor[T, S]) (scheme[T, S], bool) {
	var best scheme[T, S]
	bestRatio := 1.0
	for _, candidate := range c.Schemes() {
		if c.IsExcluded(ctx, candidate.Kind()) {
			continue
		}
		ratio, ok := candidate.Estimate(stats, ctx)
		if !ok || ratio <= 1.0 || math.IsNaN(ratio) || math.IsInf(ratio, 0) {
			continue
		}
		if ratio > bestRatio {
			best = candidate
			bestRatio = ratio
		}
	}
	return best, best != nil
}

func estimateBySample[T Integer | Float | String, S statsSource[T]](stats S, ctx planContext, build func(array.Array[T], planContext) (Codec[T], error)) (float64, bool) {
	sampledCtx := ctx.sampled()
	sample := stats.Sample(ctx)
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
