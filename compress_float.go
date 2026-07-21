package btrblocks

import (
	"github.com/axiomhq/btrblocks/array"
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
					return estimateALP[T, floatStats[T]](stats, ctx, stats.isConst)
				},
			},
			{
				kind:  CodecTypeALPRD,
				build: buildALPRD,
				estimate: func(stats floatStats[T], ctx planContext) schemeEstimate {
					if preferFloatDictionary(stats) {
						return skipEstimate()
					}
					return estimateALPRD[T, floatStats[T]](stats, ctx, stats.isConst)
				},
			},
			{
				kind: CodecTypeRunEnd,
				build: func(arr array.ArrayCore[T], ctx planContext) (EncodedArray[T], error) {
					return buildPrimitiveRunEnd(arr, ctx, array.CmpFloatBits[T], compressValues)
				},
				estimate: func(stats floatStats[T], ctx planContext) schemeEstimate {
					return estimateRunEnd[T, floatStats[T]](stats, ctx, stats.avgRunLength, primitiveCanSample(stats.Source(), ctx))
				},
			},
			{
				kind: CodecTypeSparse,
				build: func(arr array.ArrayCore[T], ctx planContext) (EncodedArray[T], error) {
					return buildFloatSparse(arr, ctx, compressValues)
				},
				estimate: func(stats floatStats[T], ctx planContext) schemeEstimate {
					return estimateSparseGeneric[T](stats, ctx, primitiveCanSample(stats.Source(), ctx))
				},
			},
			{
				kind:    CodecTypeDict,
				build:   buildDict,
				resolve: resolveDict,
				estimate: func(stats floatStats[T], ctx planContext) schemeEstimate {
					return estimateFloatDict[T](stats, ctx)
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
type floatDictionaryBuilder[T array.Float] func(array.ArrayCore[T], map[uint64]uint64, uint64, dictionaryChildBuilder[T]) (EncodedArray[T], error)

func resolveFloat32Dictionary(arr array.Array[float32], stats floatStats[float32], ctx planContext, estimate schemeEstimate, bestRatio float64) (resolvedEstimate[float32], error) {
	return resolveFloatDictionary(arr, stats, ctx, estimate, bestRatio, compressFloat32Core, encodeFloat32Dict)
}

func resolveFloat64Dictionary(arr array.Array[float64], stats floatStats[float64], ctx planContext, estimate schemeEstimate, bestRatio float64) (resolvedEstimate[float64], error) {
	return resolveFloatDictionary(arr, stats, ctx, estimate, bestRatio, compressFloat64Core, encodeFloat64Dict)
}

func resolveFloatDictionary[T array.Float](arr array.Array[T], stats floatStats[T], ctx planContext, estimate schemeEstimate, bestRatio float64, compressValues childCompressor[T], buildDictionary floatDictionaryBuilder[T]) (resolvedEstimate[T], error) {
	if estimate.ratio < bestRatio*floatDictionaryRefineMargin {
		return resolvedEstimate[T]{}, nil
	}
	var ordinals map[uint64]uint64
	if stats.distinct != nil {
		// Go map iteration order is deliberately unstable. Reconstruct the
		// retained exact dictionary in first-seen order so identical inputs
		// produce identical bytes.
		ordinals = make(map[uint64]uint64, len(stats.distinct))
		for i := range arr.Length() {
			bits := array.FloatBits(arr.ValueAt(i))
			if _, exists := ordinals[bits]; exists {
				continue
			}
			ordinals[bits] = uint64(len(ordinals))
			if len(ordinals) == len(stats.distinct) {
				break
			}
		}
	}
	dictionary, err := buildDictionary(arr, ordinals, stats.distinctEstimate, dictionaryChildren[T]{ctx: ctx, compressValues: compressValues})
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
