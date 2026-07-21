package compress

import (
	"github.com/axiomhq/btrblocks/array"
	"github.com/axiomhq/btrblocks/codec"
)

// floatCompressor owns the floating-point schemes and stats policy.
type floatCompressor[T array.Float] struct {
	schemes schemeSet[T, floatStats[T]]
}

func float32Compressor() floatCompressor[float32] {
	return floatCompressor[float32]{schemes: floatSchemes(
		compressFloat32Core,
		buildALP32,
		buildALPRD32,
		buildFloat32Dict,
		resolveFloat32Dictionary,
	)}
}

func float64Compressor() floatCompressor[float64] {
	return floatCompressor[float64]{schemes: floatSchemes(
		compressFloat64Core,
		buildALP64,
		buildALPRD64,
		buildFloat64Dict,
		resolveFloat64Dictionary,
	)}
}

func (c floatCompressor[T]) ComputeStats(arr array.Array[T], ctx planContext) floatStats[T] {
	return computeFloatStatsForPlanner(arr, shouldCollectFrequencies(arr, ctx, c))
}

func (c floatCompressor[T]) Schemes(floatStats[T]) schemeSet[T, floatStats[T]] {
	return c.schemes
}

func floatSchemes[T array.Float](compressValues childCompressor[T], buildALP, buildALPRD, buildDict schemeBuild[T], resolveDict schemeResolve[T, floatStats[T]]) schemeSet[T, floatStats[T]] {
	return schemeSet[T, floatStats[T]]{
		count: 6,
		values: [maxSchemeCount]scheme[T, floatStats[T]]{
			{
				kind:  CodecTypeConst,
				build: buildFloatConst[T],
				estimate: func(stats floatStats[T], ctx planContext) schemeEstimate {
					return estimateConst(stats.Source(), ctx, stats.isConst)
				},
			},
			{
				kind:  CodecTypeALP,
				build: buildALP,
				estimate: func(stats floatStats[T], ctx planContext) schemeEstimate {
					if preferFloatDictionary(stats) {
						return skipEstimate()
					}
					return estimateALP(stats, ctx, stats.isConst)
				},
			},
			{
				kind:  CodecTypeALPRD,
				build: buildALPRD,
				estimate: func(stats floatStats[T], ctx planContext) schemeEstimate {
					if preferFloatDictionary(stats) {
						return skipEstimate()
					}
					return estimateALPRD(stats, ctx, stats.isConst)
				},
			},
			{
				kind: CodecTypeRunEnd,
				build: func(arr array.ArrayCore[T], ctx planContext) (EncodedArray[T], error) {
					return buildPrimitiveRunEnd(arr, ctx, compressValues)
				},
				estimate: func(stats floatStats[T], ctx planContext) schemeEstimate {
					return estimateRunEnd(stats, ctx, stats.avgRunLength, primitiveCanSample(stats.Source(), ctx))
				},
			},
			{
				kind: CodecTypeSparse,
				build: func(arr array.ArrayCore[T], ctx planContext) (EncodedArray[T], error) {
					return buildFloatSparse(arr, ctx, compressValues)
				},
				estimate: func(stats floatStats[T], ctx planContext) schemeEstimate {
					return estimateSparseGeneric(stats, ctx, primitiveCanSample(stats.Source(), ctx))
				},
			},
			{
				kind:    CodecTypeDict,
				build:   buildDict,
				resolve: resolveDict,
				estimate: func(stats floatStats[T], ctx planContext) schemeEstimate {
					return estimateFloatDict(stats, ctx)
				},
			},
		},
	}
}

func preferFloatDictionary[T array.Float](stats floatStats[T]) bool {
	n := stats.Source().Length()
	return stats.distinctEstimate > 1 && stats.distinctEstimate <= 8192 && stats.distinctEstimate*16 <= n
}

func (floatCompressor[T]) IsExcluded(ctx planContext, kind CodecType) bool {
	return ctx.excludesFloat(kind)
}

func (floatCompressor[T]) RawEncodedSize(arr array.ArrayCore[T]) uint64 {
	return primitiveRawEncodedSize(arr)
}

const floatDictionaryRefineMargin = 0.90

// resolveFloatDictionary performs the exact dictionary build only after ALP and
// ALPRD have produced their sampled scores. The full-column distinct sketch is
// accurate enough to avoid rebuilding dictionaries that are clearly unable to
// beat the incumbent, while the exact output is retained when it wins so it is
// never built twice.
type floatDictionaryBuilder[T array.Float] func(array.ArrayCore[T], uint64, codec.DictionaryChildBuilder[T], ...codec.BuildOptions) (EncodedArray[T], error)

func resolveFloat32Dictionary(arr array.Array[float32], stats floatStats[float32], ctx planContext, estimate schemeEstimate, bestRatio float64) (resolvedEstimate[float32], error) {
	return resolveFloatDictionary(arr, stats, ctx, estimate, bestRatio, compressFloat32Core, codec.EncodeFloat32Dict)
}

func resolveFloat64Dictionary(arr array.Array[float64], stats floatStats[float64], ctx planContext, estimate schemeEstimate, bestRatio float64) (resolvedEstimate[float64], error) {
	return resolveFloatDictionary(arr, stats, ctx, estimate, bestRatio, compressFloat64Core, codec.EncodeFloat64Dict)
}

func resolveFloatDictionary[T array.Float](arr array.Array[T], stats floatStats[T], ctx planContext, estimate schemeEstimate, bestRatio float64, compressValues childCompressor[T], buildDictionary floatDictionaryBuilder[T]) (resolvedEstimate[T], error) {
	if estimate.ratio < bestRatio*floatDictionaryRefineMargin {
		return resolvedEstimate[T]{}, nil
	}
	dictionary, err := buildDictionary(arr, stats.distinctEstimate, dictionaryChildren[T]{ctx: ctx, compressValues: compressValues}, ctx.buildOptions())
	if err != nil {
		if isExpectedBuildError(err) {
			return resolvedEstimate[T]{}, nil
		}
		return resolvedEstimate[T]{}, err
	}
	if dictionary.BinarySize() == 0 {
		return resolvedEstimate[T]{}, nil
	}
	return resolvedEstimate[T]{
		encoded: dictionary,
		ratio:   float64(primitiveRawEncodedSize(arr)) / float64(dictionary.BinarySize()),
		ok:      true,
	}, nil
}
