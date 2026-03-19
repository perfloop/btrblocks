package btrblocks

import "github.com/axiomhq/btrblocks/array"

func compressStringArray(arr array.Array[string], ctx planContext) (Codec[string], error) {
	stats := computeStringStats(arr)
	candidates := []candidate[string]{
		{
			kind:     CodecTypeConst,
			estimate: estimateConst[string](stats.isConst),
			build: func(arr array.Array[string], _ planContext) (Codec[string], error) {
				return newConstStringCodec(arr)
			},
		},
		{
			kind:     CodecTypeDict,
			estimate: estimateStringDict[string](stats.distinctRatio),
			build:    buildStringDictCodec[string],
		},
		{
			kind:     CodecTypeSparse,
			estimate: estimateSparse[string](stats.isConst, stats.topCount, stats.topValue, cmpStrings[string]),
			build: func(arr array.Array[string], ctx planContext) (Codec[string], error) {
				return buildSparseCodec(arr, ctx, stats.topValue, cmpStrings[string])
			},
		},
		{
			kind:     CodecTypeRunEnd,
			estimate: estimateRunEnd[string](stats.avgRunLength, cmpStrings[string]),
			build: func(arr array.Array[string], ctx planContext) (Codec[string], error) {
				return buildRunEndCodec(arr, ctx, cmpStrings[string])
			},
		},
	}
	return selectBest(arr, ctx, candidates)
}
