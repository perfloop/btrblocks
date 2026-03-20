package btrblocks

import "github.com/axiomhq/btrblocks/array"

type stringCompressor struct{}

func (stringCompressor) ComputeStats(arr array.Array[string]) baseStats[string] {
	return computeStringStats(arr)
}

func (stringCompressor) Schemes() []scheme[string, baseStats[string]] {
	return []scheme[string, baseStats[string]]{
		registeredScheme[string, baseStats[string]]{
			kind: CodecTypeConst,
			estimate: func(stats baseStats[string], ctx planContext) (float64, bool) {
				return estimateConst[string](stats.isConst)(stats.Source(), ctx)
			},
			build: func(arr array.Array[string], _ planContext) (Codec[string], error) {
				return newConstStringCodec(arr)
			},
		},
		registeredScheme[string, baseStats[string]]{
			kind: CodecTypeDict,
			estimate: func(stats baseStats[string], ctx planContext) (float64, bool) {
				return estimateStringDict[baseStats[string]](stats.distinctRatio)(stats, ctx)
			},
			build: buildStringDictCodec[string],
		},
		registeredScheme[string, baseStats[string]]{
			kind: CodecTypeRunEnd,
			estimate: func(stats baseStats[string], ctx planContext) (float64, bool) {
				return estimateRunEnd[string, baseStats[string]](stats.avgRunLength, cmpStrings[string])(stats, ctx)
			},
			build: func(arr array.Array[string], ctx planContext) (Codec[string], error) {
				return buildRunEndCodec(arr, ctx, cmpStrings[string])
			},
		},
	}
}

func (stringCompressor) IsExcluded(ctx planContext, kind CodeType) bool {
	return ctx.excludesString(kind)
}
