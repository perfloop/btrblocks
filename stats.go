package btrblocks

import "github.com/axiomhq/btrblocks/array"

// maxRetainedDistinctValues bounds the exact dictionary retained during the
// initial stats pass. Larger dictionaries are built only when sample planning
// selects dictionary encoding, avoiding a large hash table for every page.
const maxRetainedDistinctValues = 256

func distinctInitialCapacity(n uint64) int {
	if n < maxRetainedDistinctValues {
		return int(n)
	}
	return maxRetainedDistinctValues
}

// baseStats holds type-agnostic planner statistics for one array. Samples are
// intentionally absent: the selector creates one lazily only when an eligible
// scheme reaches the deferred sampling pass.
type baseStats[T array.Integer | array.Float | array.String] struct {
	src           array.Array[T]
	isConst       bool
	distinctCount uint64
	avgRunLength  float64
	mostFrequent  uint64
}

func (s baseStats[T]) Source() array.Array[T] {
	return s.src
}

func (s baseStats[T]) IsConstant() bool { return s.isConst }

func (s baseStats[T]) MostFrequentCount() uint64 { return s.mostFrequent }
