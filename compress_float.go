package btrblocks

import "github.com/axiomhq/btrblocks/array"

// floatCompressor registers the dense floating-point schemes and stats policy.
type floatCompressor[T Float] struct{}

func (c *floatCompressor[T]) ComputeStats(arr array.Array[T]) floatStats[T] {
	return computeFloatStats(arr)
}

func (*floatCompressor[T]) DefaultScheme() scheme[T, floatStats[T]] {
	return rawScheme[T, floatStats[T]]()
}

func (c *floatCompressor[T]) Schemes(stats floatStats[T]) []scheme[T, floatStats[T]] {
	return []scheme[T, floatStats[T]]{
		registeredScheme[T, floatStats[T]]{
			kind: CodecTypeConst,
			estimate: func(stats floatStats[T], ctx planContext) (float64, bool) {
				return estimateConst[T](stats.base.isConst)(stats.Source(), ctx)
			},
			build: func(arr array.ArrayCore[T], _ planContext) (EncodedArray[T], error) {
				return newConstFloatArray(arr)
			},
		},
		registeredScheme[T, floatStats[T]]{
			kind: CodecTypeALP,
			estimate: func(stats floatStats[T], ctx planContext) (float64, bool) {
				return estimateALP[T, floatStats[T]](stats.base.isConst)(stats, ctx)
			},
			build: buildALPArray[T],
		},
		registeredScheme[T, floatStats[T]]{
			kind: CodecTypeDict,
			estimate: func(stats floatStats[T], ctx planContext) (float64, bool) {
				return estimateFloatDict[T, floatStats[T]](stats.base.distinctCount, stats.Source().Length())(stats, ctx)
			},
			build: func(arr array.ArrayCore[T], ctx planContext) (EncodedArray[T], error) {
				return buildFloatDictFromDistinct(arr, stats.distinct, ctx)
			},
		},
		registeredScheme[T, floatStats[T]]{
			kind: CodecTypeALPRD,
			estimate: func(stats floatStats[T], ctx planContext) (float64, bool) {
				return estimateALPRD[T, floatStats[T]](stats.base.isConst)(stats, ctx)
			},
			build: buildALPRDArray[T],
		},
		registeredScheme[T, floatStats[T]]{
			kind: CodecTypeRunEnd,
			estimate: func(stats floatStats[T], ctx planContext) (float64, bool) {
				return estimateRunEnd[T, floatStats[T]](stats.base.avgRunLength, cmpFloatEq[T])(stats, ctx)
			},
			build: func(arr array.ArrayCore[T], ctx planContext) (EncodedArray[T], error) {
				return buildRunEndArray(arr, ctx, cmpFloatBits[T])
			},
		},
	}
}

func (*floatCompressor[T]) IsExcluded(ctx planContext, kind CodeType) bool {
	return ctx.excludesFloat(kind)
}
