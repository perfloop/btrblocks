package btrblocks

import "github.com/axiomhq/btrblocks/array"

type signedIntCompressor[T SignedInteger] struct{}

func (signedIntCompressor[T]) ComputeStats(arr array.Array[T]) signedStats[T] {
	return computeSignedStats(arr)
}

func (signedIntCompressor[T]) Schemes() []scheme[T, signedStats[T]] {
	return []scheme[T, signedStats[T]]{
		registeredScheme[T, signedStats[T]]{
			kind: CodecTypeConst,
			estimate: func(stats signedStats[T], ctx planContext) (float64, bool) {
				return estimateConst[T](stats.base.isConst)(stats.Source(), ctx)
			},
			build: func(arr array.Array[T], _ planContext) (Codec[T], error) {
				return newConstIntegerCodec(arr)
			},
		},
		registeredScheme[T, signedStats[T]]{
			kind: CodecTypeSequence,
			estimate: func(stats signedStats[T], ctx planContext) (float64, bool) {
				return estimateSequence[T](stats.Source(), ctx)
			},
			build: func(arr array.Array[T], _ planContext) (Codec[T], error) {
				return newSequenceCodec(arr)
			},
		},
		registeredScheme[T, signedStats[T]]{
			kind: CodecTypeZigZag,
			estimate: func(stats signedStats[T], ctx planContext) (float64, bool) {
				return estimateZigZag[T, signedStats[T]](stats.hasNegative)(stats, ctx)
			},
			build: buildZigZagCodec[T],
		},
		registeredScheme[T, signedStats[T]]{
			kind: CodecTypeDict,
			estimate: func(stats signedStats[T], ctx planContext) (float64, bool) {
				return estimateIntegerDict[T, signedStats[T]](stats.base.distinctRatio)(stats, ctx)
			},
			build: buildIntegerDictCodec[T],
		},
		registeredScheme[T, signedStats[T]]{
			kind: CodecTypeRunEnd,
			estimate: func(stats signedStats[T], ctx planContext) (float64, bool) {
				return estimateRunEnd[T, signedStats[T]](stats.base.avgRunLength, cmpIntegers[T])(stats, ctx)
			},
			build: func(arr array.Array[T], ctx planContext) (Codec[T], error) {
				return buildRunEndCodec(arr, ctx, cmpIntegers[T])
			},
		},
	}
}

func (signedIntCompressor[T]) IsExcluded(ctx planContext, kind CodeType) bool {
	return ctx.excludesInteger(kind)
}
