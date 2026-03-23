package btrblocks

import "github.com/axiomhq/btrblocks/array"

// signedStats extends baseStats with negative-value tracking for signed arrays.
type signedStats[T SignedInteger] struct {
	base        baseStats[T]
	hasNegative bool
}

func (s signedStats[T]) Source() array.Array[T] {
	return s.base.Source()
}

func (s signedStats[T]) Sample(ctx planContext) array.Array[T] {
	return s.base.Sample(ctx)
}

// computeSignedStats matches the unsigned path but also tracks whether any
// negative value was observed so zigzag can be gated without a second pass.
func computeSignedStats[T SignedInteger](arr array.Array[T]) signedStats[T] {
	n := arr.Length()
	if n == 0 {
		return signedStats[T]{
			base: baseStats[T]{
				src: arr,
			},
		}
	}

	counts := make(map[T]uint64, 256)
	runs := uint64(1)
	prev := arr.ValueAt(0)
	hasNegative := prev < 0
	counts[prev]++

	for i := uint64(1); i < n; i++ {
		v := arr.ValueAt(i)
		counts[v]++
		if !hasNegative && v < 0 {
			hasNegative = true
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
	}
}
