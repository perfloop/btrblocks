package btrblocks

import "github.com/axiomhq/btrblocks/array"

const (
	distinctRatioThreshold = 0.5
)

// baseStats holds type-agnostic planner statistics for one array.
type baseStats[T Integer | Float | String] struct {
	src            array.Array[T]
	isConst       bool
	distinctCount uint64
	distinctRatio float64
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

func isConstArray[T Integer | Float | String](arr array.Array[T], cmp cmpFn[T]) bool {
	if arr.Length() == 0 {
		return false
	}
	value := arr.ValueAt(0)
	for i := uint64(1); i < arr.Length(); i++ {
		if !cmp(value, arr.ValueAt(i)) {
			return false
		}
	}
	return true
}

func avgRunLength[T Integer | Float | String](arr array.Array[T], cmp cmpFn[T]) float64 {
	n := arr.Length()
	if n == 0 {
		return 0
	}
	runs := uint64(1)
	prev := arr.ValueAt(0)
	for i := uint64(1); i < n; i++ {
		value := arr.ValueAt(i)
		if !cmp(prev, value) {
			runs++
			prev = value
		}
	}
	return float64(n) / float64(runs)
}
