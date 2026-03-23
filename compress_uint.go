package btrblocks

import "github.com/axiomhq/btrblocks/array"

// unsignedIntCompressor registers the dense unsigned-integer schemes and stats policy.
type unsignedIntCompressor[T UnsignedInteger] struct{}

func (unsignedIntCompressor[T]) ComputeStats(arr array.Array[T]) unsignedStats[T] {
	return computeUnsignedStats(arr)
}

func (unsignedIntCompressor[T]) DefaultScheme() scheme[T, unsignedStats[T]] {
	return rawScheme[T, unsignedStats[T]]()
}

func (unsignedIntCompressor[T]) Schemes() []scheme[T, unsignedStats[T]] {
	return []scheme[T, unsignedStats[T]]{
		registeredScheme[T, unsignedStats[T]]{
			kind: CodecTypeConst,
			estimate: func(stats unsignedStats[T], ctx planContext) (float64, bool) {
				return estimateConst[T](stats.base.isConst)(stats.Source(), ctx)
			},
			build: func(arr array.Array[T], _ planContext) (EncodedArray[T], error) {
				return newConstIntegerArray(arr)
			},
		},
		registeredScheme[T, unsignedStats[T]]{
			kind: CodecTypeSequence,
			estimate: func(stats unsignedStats[T], ctx planContext) (float64, bool) {
				return estimateSequence[T](stats.Source(), ctx)
			},
			build: func(arr array.Array[T], _ planContext) (EncodedArray[T], error) {
				return newSequenceArray(arr)
			},
		},
		registeredScheme[T, unsignedStats[T]]{
			kind: CodecTypeFor,
			estimate: func(stats unsignedStats[T], ctx planContext) (float64, bool) {
				return estimateFoR[T, unsignedStats[T]](stats.min, stats.max)(stats, ctx)
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
				return estimateIntegerDict[T, unsignedStats[T]](stats.base.distinctCount, stats.base.avgRunLength)(stats, ctx)
			},
			build: buildIntegerDictArray[T],
		},
		registeredScheme[T, unsignedStats[T]]{
			kind: CodecTypeRunEnd,
			estimate: func(stats unsignedStats[T], ctx planContext) (float64, bool) {
				return estimateRunEnd[T, unsignedStats[T]](stats.base.avgRunLength, cmpIntegers[T])(stats, ctx)
			},
			build: func(arr array.Array[T], ctx planContext) (EncodedArray[T], error) {
				return buildRunEndArray(arr, ctx, cmpIntegers[T])
			},
		},
	}
}

func (unsignedIntCompressor[T]) IsExcluded(ctx planContext, kind CodeType) bool {
	return ctx.excludesInteger(kind)
}
