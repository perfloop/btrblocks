package btrblocks

import "github.com/axiomhq/btrblocks/array"

// unsignedStats extends baseStats with min/max bounds for unsigned arrays.
type unsignedStats[T UnsignedInteger] struct {
	base baseStats[T]
	min  T
	max  T
}

func (s unsignedStats[T]) Source() array.Array[T] {
	return s.base.Source()
}

func (s unsignedStats[T]) Sample(ctx planContext) array.Array[T] {
	return s.base.Sample(ctx)
}

// computeUnsignedStats scans the full column once and keeps exact numeric
// cardinality, min/max, top value, and run-length statistics. Dict planning for
// integers uses exact distinct counts in the Rust reference, so we keep this
// path exact as well.
func computeUnsignedStats[T UnsignedInteger](arr array.Array[T]) (unsignedStats[T], intDistinctValues[T]) {
	n := arr.Length()
	if n == 0 {
		return unsignedStats[T]{
			base: baseStats[T]{
				src: arr,
			},
		}, intDistinctValues[T]{}
	}

	type entry struct {
		count   uint64
		ordinal uint64
	}

	counts := make(map[T]entry, 256)
	var distinctValues []T
	runs := uint64(1)
	prev := arr.ValueAt(0)
	minValue := prev
	maxValue := prev
	counts[prev] = entry{count: 1, ordinal: 0}
	distinctValues = append(distinctValues, prev)

	for i := uint64(1); i < n; i++ {
		v := arr.ValueAt(i)
		e, exists := counts[v]
		if !exists {
			e = entry{ordinal: uint64(len(distinctValues))}
			distinctValues = append(distinctValues, v)
		}
		e.count++
		counts[v] = e
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

	var topValue T
	var topCount uint64
	for v, e := range counts {
		if e.count > topCount {
			topValue = v
			topCount = e.count
		}
	}

	byKey := make(map[T]uint64, len(counts))
	for v, e := range counts {
		byKey[v] = e.ordinal
	}

	return unsignedStats[T]{
		base: baseStats[T]{
			src:           arr,
			isConst:       len(counts) == 1 && topCount == n,
			distinctCount: uint64(len(counts)),
			distinctRatio: float64(len(counts)) / float64(n),
			avgRunLength:  float64(n) / float64(runs),
			topValue:      topValue,
			topCount:      topCount,
		},
		min: minValue,
		max: maxValue,
	}, intDistinctValues[T]{byKey: byKey, values: distinctValues}
}
