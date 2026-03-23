package btrblocks

import "github.com/axiomhq/btrblocks/array"

// stringCompressor registers the dense string schemes and stats policy.
type stringCompressor struct{}

func (stringCompressor) ComputeStats(arr array.Array[string]) stringStats {
	return computeStringStats(arr)
}

func (stringCompressor) Schemes() []scheme[string, stringStats] {
	return []scheme[string, stringStats]{
		registeredScheme[string, stringStats]{
			kind: CodecTypeConst,
			estimate: func(stats stringStats, ctx planContext) (float64, bool) {
				return estimateConst[string](isConstArray(stats.Source(), cmpStrings[string]))(stats.Source(), ctx)
			},
			build: func(arr array.Array[string], _ planContext) (EncodedArray[string], error) {
				return newConstStringArray(arr)
			},
		},
		registeredScheme[string, stringStats]{
			kind: CodecTypeDict,
			estimate: func(stats stringStats, ctx planContext) (float64, bool) {
				return estimateStringDict[stringStats](stats.estimatedDistinctCount)(stats, ctx)
			},
			build: buildStringDictArray[string],
		},
		registeredScheme[string, stringStats]{
			kind: CodecTypeRunEnd,
			estimate: func(stats stringStats, ctx planContext) (float64, bool) {
				return estimateRunEnd[string, stringStats](avgRunLength(stats.Source(), cmpStrings[string]), cmpStrings[string])(stats, ctx)
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
