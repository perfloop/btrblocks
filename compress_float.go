package btrblocks

import "github.com/axiomhq/btrblocks/array"

// floatCompressor registers the dense floating-point schemes and stats policy.
type floatCompressor[T Float] struct{}

func (floatCompressor[T]) ComputeStats(arr array.Array[T]) baseStats[T] {
	return computeFloatStats(arr)
}

func (floatCompressor[T]) DefaultScheme() scheme[T, baseStats[T]] {
	return rawScheme[T, baseStats[T]]()
}

func (floatCompressor[T]) Schemes() []scheme[T, baseStats[T]] {
	return []scheme[T, baseStats[T]]{
		registeredScheme[T, baseStats[T]]{
			kind: CodecTypeConst,
			estimate: func(stats baseStats[T], ctx planContext) (float64, bool) {
				return estimateConst[T](stats.isConst)(stats.Source(), ctx)
			},
			build: func(arr array.Array[T], _ planContext) (EncodedArray[T], error) {
				return newConstFloatArray(arr)
			},
		},
		registeredScheme[T, baseStats[T]]{
			kind: CodecTypeALP,
			estimate: func(stats baseStats[T], ctx planContext) (float64, bool) {
				return estimateALP[T, baseStats[T]](stats.isConst)(stats, ctx)
			},
			build: buildALPArray[T],
		},
		registeredScheme[T, baseStats[T]]{
			kind: CodecTypeDict,
			estimate: func(stats baseStats[T], ctx planContext) (float64, bool) {
				return estimateFloatDict[T, baseStats[T]](stats.distinctRatio)(stats, ctx)
			},
			build: buildFloatDictArray[T],
		},
		registeredScheme[T, baseStats[T]]{
			kind: CodecTypeRunEnd,
			estimate: func(stats baseStats[T], ctx planContext) (float64, bool) {
				return estimateRunEnd[T, baseStats[T]](stats.avgRunLength, cmpFloatRuns[T])(stats, ctx)
			},
			build: func(arr array.Array[T], ctx planContext) (EncodedArray[T], error) {
				return buildRunEndArray(arr, ctx, cmpFloats[T])
			},
		},
	}
}

func (floatCompressor[T]) IsExcluded(ctx planContext, kind CodeType) bool {
	return ctx.excludesFloat(kind)
}
