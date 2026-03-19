package btrblocks

import "github.com/axiomhq/btrblocks/array"

func compressFloatArray[T Float](arr array.Array[T], ctx planContext) (Codec[T], error) {
	stats := computeFloatStats(arr)
	candidates := []candidate[T]{
		{
			kind:     CodecTypeConst,
			estimate: estimateConst[T](stats.isConst),
			build: func(arr array.Array[T], _ planContext) (Codec[T], error) {
				return newConstFloatCodec(arr)
			},
		},
		{
			kind:     CodecTypeALP,
			estimate: estimateALP[T](stats.isConst),
			build:    buildALPCodec[T],
		},
		{
			kind:     CodecTypeDict,
			estimate: estimateFloatDict[T](stats.distinctRatio),
			build:    buildFloatDictCodec[T],
		},
		{
			kind:     CodecTypeSparse,
			estimate: estimateSparse[T](stats.isConst, stats.topCount, stats.topValue, cmpFloats[T]),
			build: func(arr array.Array[T], ctx planContext) (Codec[T], error) {
				return buildSparseCodec(arr, ctx, stats.topValue, cmpFloats[T])
			},
		},
		{
			kind:     CodecTypeRunEnd,
			estimate: estimateRunEnd[T](stats.avgRunLength, cmpFloats[T]),
			build: func(arr array.Array[T], ctx planContext) (Codec[T], error) {
				return buildRunEndCodec(arr, ctx, cmpFloats[T])
			},
		},
	}
	return selectBest(arr, ctx, candidates)
}
