package btrblocks

import "github.com/axiomhq/btrblocks/array"

func compressUnsignedArray[T UnsignedInteger](arr array.Array[T], ctx planContext) (Codec[T], error) {
	stats := computeUnsignedStats(arr)

	candidates := []candidate[T]{
		{
			kind:     CodecTypeConst,
			estimate: estimateConst[T](stats.base.isConst),
			build: func(arr array.Array[T], _ planContext) (Codec[T], error) {
				return newConstIntegerCodec(arr)
			},
		},
		{
			kind:     CodecTypeSequence,
			estimate: estimateSequence[T],
			build: func(arr array.Array[T], _ planContext) (Codec[T], error) {
				return newSequenceCodec(arr)
			},
		},
		{
			kind:     CodecTypeBitpack,
			estimate: estimateBitpack[T],
			build: func(arr array.Array[T], _ planContext) (Codec[T], error) {
				return newBitpackCodec(arr), nil
			},
		},
		{
			kind:     CodecTypeFor,
			estimate: estimateFoR[T](stats.min, stats.max),
			build:    buildFoRCodec[T],
		},
		{
			kind:     CodecTypeSparse,
			estimate: estimateSparse[T](stats.base.isConst, stats.base.topCount, stats.base.topValue, cmpIntegers[T]),
			build: func(arr array.Array[T], ctx planContext) (Codec[T], error) {
				return buildSparseCodec(arr, ctx, stats.base.topValue, cmpIntegers[T])
			},
		},
		{
			kind:     CodecTypeDict,
			estimate: estimateIntegerDict[T](stats.base.distinctRatio),
			build:    buildIntegerDictCodec[T],
		},
		{
			kind:     CodecTypeRunEnd,
			estimate: estimateRunEnd[T](stats.base.avgRunLength, cmpIntegers[T]),
			build: func(arr array.Array[T], ctx planContext) (Codec[T], error) {
				return buildRunEndCodec(arr, ctx, cmpIntegers[T])
			},
		},
	}
	return selectBest(arr, ctx, candidates)
}
