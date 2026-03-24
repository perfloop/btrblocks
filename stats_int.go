package btrblocks

import "github.com/axiomhq/btrblocks/array"

// intDistinctValues maps integer values to their ordinal index assigned during
// stats generation. Dict encoding can reuse this instead of re-scanning.
type intDistinctValues[T Integer] struct {
	byKey  map[T]uint64
	values []T
}

// signedStats extends baseStats with negative-value tracking for signed arrays.
type signedStats[T SignedInteger] struct {
	base        baseStats[T]
	hasNegative bool
	min         T
	max         T
}

func (s signedStats[T]) Source() array.Array[T] {
	return s.base.Source()
}

func (s signedStats[T]) Sample(ctx planContext) array.Array[T] {
	return s.base.Sample(ctx)
}

// computeSignedStats matches the unsigned path but also tracks whether any
// negative value was observed so zigzag can be gated without a second pass.
func computeSignedStats[T SignedInteger](arr array.Array[T]) (signedStats[T], intDistinctValues[T]) {
	n := arr.Length()
	if n == 0 {
		return signedStats[T]{
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
	hasNegative := prev < 0
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

	return signedStats[T]{
		base: baseStats[T]{
			src:           arr,
			isConst:       len(counts) == 1 && topCount == n,
			distinctCount: uint64(len(counts)),
			distinctRatio: float64(len(counts)) / float64(n),
			avgRunLength:  float64(n) / float64(runs),
			topValue:      topValue,
			topCount:      topCount,
		},
		hasNegative: hasNegative,
		min:         minValue,
		max:         maxValue,
	}, intDistinctValues[T]{byKey: byKey, values: distinctValues}
}
