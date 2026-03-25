package btrblocks

import "github.com/axiomhq/btrblocks/array"

// unsignedStats extends baseStats with min/max bounds for unsigned arrays.
type unsignedStats[T UnsignedInteger] struct {
	base     baseStats[T]
	distinct map[T]uint64 // value → ordinal index, used by dict build
	min      T
	max      T
}

func (s unsignedStats[T]) Source() array.Array[T] {
	return s.base.Source()
}

func (s unsignedStats[T]) Sample(ctx planContext) array.ArrayCore[T] {
	return s.base.Sample(ctx)
}

// computeUnsignedStats scans the full column once and keeps exact numeric
// cardinality, min/max, top value, and run-length statistics. The full distinct
// map is retained because it is passed directly to buildIntegerDictFromDistinct
// to avoid re-scanning the column during dict construction.
func computeUnsignedStats[T UnsignedInteger](arr array.Array[T]) unsignedStats[T] {
	n := arr.Length()
	if n == 0 {
		return unsignedStats[T]{
			base: baseStats[T]{src: arr},
		}
	}

	distinct := make(map[T]uint64, 256)
	runs := uint64(1)
	prev := arr.ValueAt(0)
	minValue := prev
	maxValue := prev
	distinct[prev] = 0

	for i := uint64(1); i < n; i++ {
		v := arr.ValueAt(i)
		if distinct != nil {
			if _, exists := distinct[v]; !exists {
				if uint64(len(distinct)) >= n/2 {
					// Dict encoding is not viable — more than n/2 distinct values.
					// Nil the map to bound stats memory for high-cardinality columns.
					// Safe because the dict estimator rejects at this threshold, so
					// the dict build function is never called with a nil map.
					distinct = nil
				} else {
					distinct[v] = uint64(len(distinct))
				}
			}
		}
		if v < minValue {
			minValue = v
		}
		if v > maxValue {
			maxValue = v
		}
		if v != prev {
			runs++
			prev = v
		}
	}

	// When distinct is nil, the true count exceeded n/2. Report n so downstream
	// estimators (dict, const) see a value that triggers their rejection guards.
	dc := uint64(len(distinct))
	if distinct == nil {
		dc = n
	}

	return unsignedStats[T]{
		base: baseStats[T]{
			src:           arr,
			isConst:       len(distinct) == 1,
			distinctCount: dc,
			avgRunLength:  float64(n) / float64(runs),
		},
		distinct: distinct,
		min:      minValue,
		max:      maxValue,
	}
}
