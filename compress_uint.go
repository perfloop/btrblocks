package btrblocks

import "github.com/axiomhq/btrblocks/array"

// unsignedIntCompressor registers the dense unsigned-integer schemes and stats policy.
type unsignedIntCompressor[T UnsignedInteger] struct{}

func (c *unsignedIntCompressor[T]) ComputeStats(arr array.Array[T]) unsignedStats[T] {
	return computeUnsignedStats(arr)
}

func (*unsignedIntCompressor[T]) DefaultScheme() scheme[T, unsignedStats[T]] {
	return rawScheme[T, unsignedStats[T]]()
}

func (c *unsignedIntCompressor[T]) Schemes(stats unsignedStats[T]) []scheme[T, unsignedStats[T]] {
	return []scheme[T, unsignedStats[T]]{
		registeredScheme[T, unsignedStats[T]]{
			kind: CodecTypeConst,
			estimate: func(stats unsignedStats[T], ctx planContext) (float64, bool) {
				return estimateConst(stats.Source(), ctx, stats.base.isConst)
			},
			build: func(arr array.ArrayCore[T], _ planContext) (EncodedArray[T], error) {
				return newConstIntegerArray(arr)
			},
		},
		registeredScheme[T, unsignedStats[T]]{
			kind: CodecTypeSequence,
			estimate: func(stats unsignedStats[T], ctx planContext) (float64, bool) {
				return estimateSequence[T](stats.Source(), ctx)
			},
			build: func(arr array.ArrayCore[T], _ planContext) (EncodedArray[T], error) {
				return newSequenceArray(arr)
			},
		},
		registeredScheme[T, unsignedStats[T]]{
			kind: CodecTypeFor,
			estimate: func(stats unsignedStats[T], ctx planContext) (float64, bool) {
				return estimateFoR(ctx, stats.min, stats.max)
			},
			build: buildFoRArray[T],
		},
		registeredScheme[T, unsignedStats[T]]{
			kind: CodecTypeBitpack,
			estimate: func(stats unsignedStats[T], ctx planContext) (float64, bool) {
				return estimateBitpack[T](stats, ctx)
			},
			build: buildBitPackedArray[T],
		},
		registeredScheme[T, unsignedStats[T]]{
			kind: CodecTypeDict,
			estimate: func(stats unsignedStats[T], ctx planContext) (float64, bool) {
				return estimateIntegerDict[T, unsignedStats[T]](stats, ctx, stats.base.distinctCount, stats.base.avgRunLength)
			},
			build: func(arr array.ArrayCore[T], ctx planContext) (EncodedArray[T], error) {
				return buildIntegerDictFromDistinct(arr, stats.distinct, ctx)
			},
		},
		registeredScheme[T, unsignedStats[T]]{
			kind: CodecTypeRunEnd,
			estimate: func(stats unsignedStats[T], ctx planContext) (float64, bool) {
				return estimateRunEnd[T, unsignedStats[T]](stats, ctx, stats.base.avgRunLength, cmpIntegers[T])
			},
			build: func(arr array.ArrayCore[T], ctx planContext) (EncodedArray[T], error) {
				return buildRunEndArray(arr, ctx, cmpIntegers[T])
			},
		},
	}
}

func (*unsignedIntCompressor[T]) IsExcluded(ctx planContext, kind CodeType) bool {
	return ctx.excludesInteger(kind)
}
