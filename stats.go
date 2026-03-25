package btrblocks

import "github.com/axiomhq/btrblocks/array"

// baseStats holds type-agnostic planner statistics for one array.
// The sample is computed once during stats construction and reused across
// all scheme estimations, avoiding redundant partitioning and slicing.
type baseStats[T Integer | Float | String] struct {
	src           array.Array[T]
	cached        array.ArrayCore[T]
	isConst       bool
	distinctCount uint64
	avgRunLength  float64
}

func (s baseStats[T]) Source() array.Array[T] {
	return s.src
}

func (s baseStats[T]) Sample(ctx planContext) array.ArrayCore[T] {
	if ctx.isSample {
		return s.src
	}
	return s.cached
}

