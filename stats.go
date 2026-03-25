package btrblocks

import "github.com/axiomhq/btrblocks/array"

// baseStats holds type-agnostic planner statistics for one array.
type baseStats[T Integer | Float | String] struct {
	src           array.Array[T]
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
	return sampleArray(s.src)
}

