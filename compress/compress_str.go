package compress

import (
	"github.com/axiomhq/btrblocks/array"
)

// stringCompressor owns the string schemes and stats policy.
type stringCompressor struct{}

func (c stringCompressor) ComputeStats(arr array.Array[string], ctx planContext) stringStats {
	return computeStringStatsForPlanner(arr, shouldCollectFrequencies(arr, ctx, c))
}

func (stringCompressor) Schemes(stringStats) schemeSet[string, stringStats] {
	return stringSchemes()
}

func stringSchemes() schemeSet[string, stringStats] {
	return schemeSet[string, stringStats]{
		count: 5,
		values: [maxSchemeCount]scheme[string, stringStats]{
			{
				kind:  CodecTypeConst,
				build: buildStringConst,
				estimate: func(stats stringStats, ctx planContext) schemeEstimate {
					return estimateConst(stats.Source(), ctx, stats.isConst)
				},
			},
			{
				kind:  CodecTypeDict,
				build: buildStringDict,
				estimate: func(stats stringStats, ctx planContext) schemeEstimate {
					return estimateStringDict(stats, ctx, stats.estimatedDistinctCount)
				},
			},
			{
				kind: CodecTypeRunEnd,
				build: func(arr array.ArrayCore[string], ctx planContext) (EncodedArray[string], error) {
					return buildStringRunEnd(arr, ctx)
				},
				estimate: func(stats stringStats, ctx planContext) schemeEstimate {
					return estimateRunEnd(stats, ctx, stats.avgRunLength, stringCanSample(stats.Source(), ctx))
				},
			},
			{
				kind:     CodecTypeFSST,
				build:    buildFSST,
				estimate: estimateFSST,
			},
			{
				kind: CodecTypeSparse,
				build: func(arr array.ArrayCore[string], ctx planContext) (EncodedArray[string], error) {
					return buildStringSparse(arr, ctx)
				},
				estimate: func(stats stringStats, ctx planContext) schemeEstimate {
					return estimateSparseGeneric(stats, ctx, stringCanSample(stats.Source(), ctx))
				},
			},
		},
	}
}

func (stringCompressor) IsExcluded(ctx planContext, kind CodecType) bool {
	return ctx.excludesString(kind)
}

func (stringCompressor) RawEncodedSize(arr array.ArrayCore[string]) uint64 {
	return stringRawEncodedSize(arr)
}
