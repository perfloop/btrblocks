package btrblocks

import (
	"github.com/axiomhq/btrblocks/array"
)

// unsignedIntCompressor owns the unsigned-integer schemes and stats policy.
type unsignedIntCompressor[T array.UnsignedInteger] struct{}

func unsignedIntegerSchemes[T array.UnsignedInteger]() schemeSet[T, intStats[T]] {
	set := integerSchemes[T](compressUnsignedCore[T], unsignedInteger, resolveUnsignedDelta[T])
	set.values[set.count] = scheme[T, intStats[T]]{
		kind:  CodecTypeBitpack,
		build: buildBitpack[T],
		estimate: func(stats intStats[T], ctx planContext) schemeEstimate {
			return estimateBitpack[T](stats, ctx)
		},
	}
	set.count++
	return set
}

func (c unsignedIntCompressor[T]) ComputeStats(arr array.Array[T], ctx planContext) intStats[T] {
	return computeIntStatsForPlanner(arr, shouldCollectFrequencies(arr, ctx, c))
}

func (unsignedIntCompressor[T]) Schemes(intStats[T]) schemeSet[T, intStats[T]] {
	return unsignedIntegerSchemes[T]()
}

func (unsignedIntCompressor[T]) IsExcluded(ctx planContext, kind CodecType) bool {
	return ctx.excludesInteger(kind)
}

func (unsignedIntCompressor[T]) RawEncodedSize(arr array.ArrayCore[T]) uint64 {
	return primitiveRawEncodedSize(arr)
}
