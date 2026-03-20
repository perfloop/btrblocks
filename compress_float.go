package btrblocks

import "github.com/axiomhq/btrblocks/array"

type floatCompressor[T Float] struct{}

func (floatCompressor[T]) ComputeStats(arr array.Array[T]) baseStats[T] {
	return computeFloatStats(arr)
}

func (floatCompressor[T]) Schemes() []scheme[T, baseStats[T]] {
	return []scheme[T, baseStats[T]]{
		registeredScheme[T, baseStats[T]]{
			kind: CodecTypeConst,
			estimate: func(stats baseStats[T], ctx planContext) (float64, bool) {
				return estimateConst[T](stats.isConst)(stats.Source(), ctx)
			},
			build: func(arr array.Array[T], _ planContext) (Codec[T], error) {
				return newConstFloatCodec(arr)
			},
		},
		registeredScheme[T, baseStats[T]]{
			kind: CodecTypeALP,
			estimate: func(stats baseStats[T], ctx planContext) (float64, bool) {
				return estimateALP[T, baseStats[T]](stats.isConst)(stats, ctx)
			},
			build: buildALPCodec[T],
		},
		registeredScheme[T, baseStats[T]]{
			kind: CodecTypeDict,
			estimate: func(stats baseStats[T], ctx planContext) (float64, bool) {
				return estimateFloatDict[T, baseStats[T]](stats.distinctRatio)(stats, ctx)
			},
			build: buildFloatDictCodec[T],
		},
		registeredScheme[T, baseStats[T]]{
			kind: CodecTypeRunEnd,
			estimate: func(stats baseStats[T], ctx planContext) (float64, bool) {
				return estimateRunEnd[T, baseStats[T]](stats.avgRunLength, cmpFloats[T])(stats, ctx)
			},
			build: func(arr array.Array[T], ctx planContext) (Codec[T], error) {
				return buildRunEndCodec(arr, ctx, cmpFloats[T])
			},
		},
	}
}

func (floatCompressor[T]) IsExcluded(ctx planContext, kind CodeType) bool {
	return ctx.excludesFloat(kind)
}
