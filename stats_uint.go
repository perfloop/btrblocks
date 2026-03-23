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
func computeUnsignedStats[T UnsignedInteger](arr array.Array[T]) unsignedStats[T] {
	n := arr.Length()
	if n == 0 {
		return unsignedStats[T]{
			base: baseStats[T]{
				src: arr,
			},
		}
	}

	counts := make(map[T]uint64, 256)
	runs := uint64(1)
	prev := arr.ValueAt(0)
	minValue := prev
	maxValue := prev
	counts[prev]++

	for i := uint64(1); i < n; i++ {
		v := arr.ValueAt(i)
		counts[v]++
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
	for v, count := range counts {
		if count > topCount {
			topValue = v
			topCount = count
		}
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
	}
}
