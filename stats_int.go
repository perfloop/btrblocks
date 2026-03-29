package btrblocks

import "github.com/axiomhq/btrblocks/array"

// signedStats extends baseStats with negative-value tracking for signed arrays.
type signedStats[T SignedInteger] struct {
	base        baseStats[T]
	distinct    map[T]uint64 // value → ordinal index, used by dict build
	hasNegative bool
	min         T
	max         T
}

func (s signedStats[T]) Source() array.Array[T] {
	return s.base.Source()
}

func (s signedStats[T]) Sample(ctx planContext) array.ArrayCore[T] {
	return s.base.Sample(ctx)
}

// computeSignedStats matches the unsigned path but also tracks whether any
// negative value was observed so zigzag can be gated without a second pass.
// The full distinct map is retained for dict construction (see stats_uint.go).
func computeSignedStats[T SignedInteger](arr array.Array[T]) signedStats[T] {
	n := arr.Length()
	if n == 0 {
		return signedStats[T]{
			base: baseStats[T]{src: arr, cached: arr},
		}
	}

	// Materialize once to avoid per-element interface dispatch.
	vals := make([]T, n)
	arr.CopyTo(vals)

	distinct := make(map[T]uint64, 256)
	runs := uint64(1)
	prev := vals[0]
	hasNegative := prev < 0
	minValue := prev
	maxValue := prev
	distinct[prev] = 0

	for _, v := range vals[1:] {
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
		if !hasNegative && v < 0 {
			hasNegative = true
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

	return signedStats[T]{
		base: baseStats[T]{
			src:           arr,
			cached:        sampleArray(arr),
			isConst:       runs == 1,
			distinctCount: dc,
			avgRunLength:  float64(n) / float64(runs),
		},
		distinct:    distinct,
		hasNegative: hasNegative,
		min:         minValue,
		max:         maxValue,
	}
}
