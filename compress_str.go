package btrblocks

import "github.com/axiomhq/btrblocks/array"

// stringCompressor registers the dense string schemes and stats policy.
type stringCompressor struct{}

func (stringCompressor) ComputeStats(arr array.Array[string]) stringStats {
	return computeStringStats(arr)
}

func (stringCompressor) DefaultScheme() scheme[string, stringStats] {
	return rawScheme[string, stringStats]()
}

func (stringCompressor) Schemes(stats stringStats) []scheme[string, stringStats] {
	return []scheme[string, stringStats]{
		registeredScheme[string, stringStats]{
			kind: CodecTypeConst,
			estimate: func(stats stringStats, ctx planContext) (float64, bool) {
				return estimateConst(stats.Source(), ctx, stats.base.isConst)
			},
			build: func(arr array.ArrayCore[string], _ planContext) (EncodedArray[string], error) {
				return newConstStringArray(arr)
			},
		},
		registeredScheme[string, stringStats]{
			kind: CodecTypeDict,
			estimate: func(stats stringStats, ctx planContext) (float64, bool) {
				return estimateStringDict(stats, ctx, stats.estimatedDistinctCount)
			},
			build: buildStringDictArray[string],
		},
		registeredScheme[string, stringStats]{
			kind: CodecTypeRunEnd,
			estimate: func(stats stringStats, ctx planContext) (float64, bool) {
				return estimateRunEnd[string, stringStats](stats, ctx, stats.base.avgRunLength, cmpStrings[string])
			},
			build: func(arr array.ArrayCore[string], ctx planContext) (EncodedArray[string], error) {
				return buildRunEndArray(arr, ctx, cmpStrings[string])
			},
		},
		registeredScheme[string, stringStats]{
			kind:     CodecTypeFSST,
			estimate: estimateFSST,
			build:    buildFSSTArray,
		},
	}
}

func (stringCompressor) IsExcluded(ctx planContext, kind CodeType) bool {
	return ctx.excludesString(kind)
}
