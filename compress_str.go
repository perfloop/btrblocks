package btrblocks

import "github.com/axiomhq/btrblocks/array"

// stringCompressor registers the dense string schemes and stats policy.
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
			build: func(arr array.Array[string], _ planContext) (EncodedArray[string], error) {
				return newConstStringArray(arr)
			},
		},
		registeredScheme[string, baseStats[string]]{
			kind: CodecTypeDict,
			estimate: func(stats baseStats[string], ctx planContext) (float64, bool) {
				return estimateStringDict[baseStats[string]](stats.distinctRatio)(stats, ctx)
			},
			build: buildStringDictArray[string],
		},
		registeredScheme[string, baseStats[string]]{
			kind: CodecTypeRunEnd,
			estimate: func(stats baseStats[string], ctx planContext) (float64, bool) {
				return estimateRunEnd[string, baseStats[string]](stats.avgRunLength, cmpStrings[string])(stats, ctx)
			},
			build: func(arr array.Array[string], ctx planContext) (EncodedArray[string], error) {
				return buildRunEndArray(arr, ctx, cmpStrings[string])
			},
		},
	}
}

func (stringCompressor) IsExcluded(ctx planContext, kind CodeType) bool {
	return ctx.excludesString(kind)
}
