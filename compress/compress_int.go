package compress

import (
	"github.com/axiomhq/btrblocks/array"
)

// integerSchemes returns the schemes shared by the signed and unsigned
// integer compressors: Const, Sequence, FoR, Delta, Dict, and RunEnd. Each
// compressor appends only its exclusive scheme (ZigZag or Bitpack).
func integerSchemes[T array.Integer](compressValues childCompressor[T], family integerFamily, resolveDelta schemeResolve[T, intStats[T]]) schemeSet[T, intStats[T]] {
	return schemeSet[T, intStats[T]]{
		count: 7,
		values: [maxSchemeCount]scheme[T, intStats[T]]{
			{
				kind:  CodecTypeConst,
				build: buildIntegerConst[T],
				estimate: func(stats intStats[T], ctx planContext) schemeEstimate {
					return estimateConst(stats.Source(), ctx, stats.isConst)
				},
			},
			{
				kind:  CodecTypeSequence,
				build: buildSequence[T],
				estimate: func(stats intStats[T], ctx planContext) schemeEstimate {
					if ctx.nullCount != 0 {
						return skipEstimate()
					}
					return estimateKnownSequence(stats.Source(), stats.isSequence, stats.sequenceBase, stats.sequenceStep)
				},
			},
			{
				kind:  CodecTypeFor,
				build: buildFoR[T],
				estimate: func(stats intStats[T], ctx planContext) schemeEstimate {
					return estimateFoR(stats.Source(), ctx, stats.min, stats.max, family)
				},
			},
			{
				kind: CodecTypeDelta,
				build: func(arr array.ArrayCore[T], ctx planContext) (EncodedArray[T], error) {
					return buildDelta(arr, ctx, compressValues)
				},
				resolve: resolveDelta,
				estimate: func(stats intStats[T], ctx planContext) schemeEstimate {
					return estimateDelta(stats, ctx, family)
				},
			},
			{
				kind: CodecTypeDict,
				build: func(arr array.ArrayCore[T], ctx planContext) (EncodedArray[T], error) {
					return buildIntegerDict(arr, ctx, compressValues)
				},
				estimate: func(stats intStats[T], ctx planContext) schemeEstimate {
					if stats.distinct == nil {
						return estimateIntegerDictBySample(stats, ctx)
					}
					return estimateIntegerDict(stats, ctx, stats.distinctCount, stats.avgRunLength)
				},
			},
			{
				kind: CodecTypeRunEnd,
				build: func(arr array.ArrayCore[T], ctx planContext) (EncodedArray[T], error) {
					return buildPrimitiveRunEnd(arr, ctx, compressValues)
				},
				estimate: func(stats intStats[T], ctx planContext) schemeEstimate {
					return estimateRunEnd(stats, ctx, stats.avgRunLength, primitiveCanSample(stats.Source(), ctx))
				},
			},
			{
				kind: CodecTypeSparse,
				build: func(arr array.ArrayCore[T], ctx planContext) (EncodedArray[T], error) {
					return buildIntegerSparse(arr, ctx, compressValues)
				},
				estimate: func(stats intStats[T], ctx planContext) schemeEstimate {
					return estimateSparseGeneric(stats, ctx, primitiveCanSample(stats.Source(), ctx))
				},
			},
		},
	}
}

// signedIntCompressor owns the signed-integer schemes and stats policy.
type signedIntCompressor[T array.SignedInteger] struct{}

func signedIntegerSchemes[T array.SignedInteger]() schemeSet[T, intStats[T]] {
	set := integerSchemes(compressSignedCore[T], signedInteger, resolveSignedDelta[T])
	set.values[set.count] = scheme[T, intStats[T]]{
		kind:  CodecTypeZigZag,
		build: buildZigZag[T],
		estimate: func(stats intStats[T], ctx planContext) schemeEstimate {
			return estimateZigZag(stats, ctx, stats.hasNegative)
		},
	}
	set.count++
	return set
}

func (c signedIntCompressor[T]) ComputeStats(arr array.Array[T], ctx planContext) intStats[T] {
	return computeIntStatsForPlanner(arr, shouldCollectFrequencies(arr, ctx, c))
}

func (signedIntCompressor[T]) Schemes(intStats[T]) schemeSet[T, intStats[T]] {
	return signedIntegerSchemes[T]()
}

func (signedIntCompressor[T]) IsExcluded(ctx planContext, kind CodecType) bool {
	return ctx.excludesInteger(kind)
}

func (signedIntCompressor[T]) RawEncodedSize(arr array.ArrayCore[T]) uint64 {
	return primitiveRawEncodedSize(arr)
}

func resolveSignedDelta[T array.SignedInteger](_ array.Array[T], stats intStats[T], _ planContext, estimate schemeEstimate, bestRatio float64) (resolvedEstimate[T], error) {
	return resolveDeltaEstimate(stats, estimate, bestRatio, signedInteger), nil
}

func resolveUnsignedDelta[T array.UnsignedInteger](_ array.Array[T], stats intStats[T], _ planContext, estimate schemeEstimate, bestRatio float64) (resolvedEstimate[T], error) {
	return resolveDeltaEstimate(stats, estimate, bestRatio, unsignedInteger), nil
}
