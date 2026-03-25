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
			base: baseStats[T]{src: arr},
		}
	}

	distinct := make(map[T]uint64, 256)
	runs := uint64(1)
	prev := arr.ValueAt(0)
	hasNegative := prev < 0
	minValue := prev
	maxValue := prev
	distinct[prev] = 0

	for i := uint64(1); i < n; i++ {
		v := arr.ValueAt(i)
		if _, exists := distinct[v]; !exists {
			distinct[v] = uint64(len(distinct))
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

	return signedStats[T]{
		base: baseStats[T]{
			src:           arr,
			isConst:       len(distinct) == 1,
			distinctCount: uint64(len(distinct)),
			distinctRatio: float64(len(distinct)) / float64(n),
			avgRunLength:  float64(n) / float64(runs),
		},
		distinct:    distinct,
		hasNegative: hasNegative,
		min:         minValue,
		max:         maxValue,
	}
}
