package btrblocks

import "github.com/axiomhq/btrblocks/array"

// signedIntCompressor registers the dense signed-integer schemes and stats policy.
type signedIntCompressor[T SignedInteger] struct{}

func (c *signedIntCompressor[T]) ComputeStats(arr array.Array[T]) signedStats[T] {
	return computeSignedStats(arr)
}

func (*signedIntCompressor[T]) DefaultScheme() scheme[T, signedStats[T]] {
	return rawScheme[T, signedStats[T]]()
}

func (c *signedIntCompressor[T]) Schemes(stats signedStats[T]) []scheme[T, signedStats[T]] {
	return []scheme[T, signedStats[T]]{
		registeredScheme[T, signedStats[T]]{
			kind: CodecTypeConst,
			estimate: func(stats signedStats[T], ctx planContext) (float64, bool) {
				return estimateConst[T](stats.base.isConst)(stats.Source(), ctx)
			},
			build: func(arr array.Array[T], _ planContext) (EncodedArray[T], error) {
				return newConstIntegerArray(arr)
			},
		},
		registeredScheme[T, signedStats[T]]{
			kind: CodecTypeSequence,
			estimate: func(stats signedStats[T], ctx planContext) (float64, bool) {
				return estimateSequence[T](stats.Source(), ctx)
			},
			build: func(arr array.Array[T], _ planContext) (EncodedArray[T], error) {
				return newSequenceArray(arr)
			},
		},
		registeredScheme[T, signedStats[T]]{
			kind: CodecTypeZigZag,
			estimate: func(stats signedStats[T], ctx planContext) (float64, bool) {
				return estimateZigZag[T, signedStats[T]](stats.hasNegative)(stats, ctx)
			},
			build: buildZigZagArray[T],
		},
		registeredScheme[T, signedStats[T]]{
			kind: CodecTypeDict,
			estimate: func(stats signedStats[T], ctx planContext) (float64, bool) {
				return estimateIntegerDict[T, signedStats[T]](stats.base.distinctCount, stats.base.avgRunLength)(stats, ctx)
			},
			build: func(arr array.Array[T], ctx planContext) (EncodedArray[T], error) {
				return buildIntegerDictFromDistinct(arr, stats.distinct, ctx)
			},
		},
		registeredScheme[T, signedStats[T]]{
			kind: CodecTypeRunEnd,
			estimate: func(stats signedStats[T], ctx planContext) (float64, bool) {
				return estimateRunEnd[T, signedStats[T]](stats.base.avgRunLength, cmpIntegers[T])(stats, ctx)
			},
			build: func(arr array.Array[T], ctx planContext) (EncodedArray[T], error) {
				return buildRunEndArray(arr, ctx, cmpIntegers[T])
			},
		},
	}
}

func (*signedIntCompressor[T]) IsExcluded(ctx planContext, kind CodeType) bool {
	return ctx.excludesInteger(kind)
}
