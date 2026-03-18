package btrblocks

import (
	"github.com/axiomhq/btrblocks/array"
)

// baseStats holds statistics computed from the full array for
// stats-based builder rejection. All fields are meaningful for
// every element type.
type baseStats[T Integer | Float | String] struct {
	isConst       bool    // true if every element is identical
	distinctRatio float64 // distinctCount / length
	avgRunLength  float64 // length / runCount
	topValue      T       // most frequent value
	topCount      uint64  // occurrence count of topValue
}

// shouldSkip returns true if a builder of the given kind should be skipped
// based on universal array statistics. Type-specific skips (FoR, Zigzag)
// are handled by the type-specific compress functions via codecExcludes.
func (s baseStats[T]) shouldSkip(kind CodecType, n uint64) bool {
	switch kind {
	case CodecTypeDict:
		return s.distinctRatio > 0.5
	case CodecTypeRunend:
		return s.avgRunLength < 4.0
	case CodecTypeSparse:
		return n == 0 || float64(s.topCount)/float64(n) < 0.9
	case CodecTypeSequence:
		return s.distinctRatio < 1.0
	case CodecTypeALP:
		return s.isConst
	default:
		return false
	}
}

// unsignedIntStats extends baseStats with unsigned-integer-specific fields.
type unsignedIntStats[T UnsignedInteger] struct {
	baseStats[T]
	min T
	max T
}

// signedIntStats extends baseStats with signed-integer-specific fields.
type signedIntStats[T SignedInteger] struct {
	baseStats[T]
	hasNegative bool
}

// computeUnsignedIntStats scans the array once to compute base stats
// plus native min/max without any interface boxing.
func computeUnsignedIntStats[T UnsignedInteger](arr array.Array[T]) unsignedIntStats[T] {
	n := arr.Length()
	if n == 0 {
		return unsignedIntStats[T]{}
	}

	var (
		counts = make(map[T]uint64, 256)
		runs   = uint64(1)
		prev   = arr.ValueAt(0)
		min    = prev
		max    = prev
	)
	counts[prev]++

	for i := uint64(1); i < n; i++ {
		v := arr.ValueAt(i)
		counts[v]++
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
		if v != prev {
			runs++
			prev = v
		}
	}

	var (
		topVal   T
		topCount uint64
	)
	for v, c := range counts {
		if c > topCount {
			topVal = v
			topCount = c
		}
	}

	return unsignedIntStats[T]{
		baseStats: baseStats[T]{
			isConst:       len(counts) == 1 && topCount == n,
			distinctRatio: float64(len(counts)) / float64(n),
			avgRunLength:  float64(n) / float64(runs),
			topValue:      topVal,
			topCount:      topCount,
		},
		min: min,
		max: max,
	}
}

// computeSignedIntStats scans the array once to compute base stats
// plus native negativity tracking without any interface boxing.
func computeSignedIntStats[T SignedInteger](arr array.Array[T]) signedIntStats[T] {
	n := arr.Length()
	if n == 0 {
		return signedIntStats[T]{}
	}

	var (
		counts = make(map[T]uint64, 256)
		runs   = uint64(1)
		prev   = arr.ValueAt(0)
		hasNeg = prev < 0
	)
	counts[prev]++

	for i := uint64(1); i < n; i++ {
		v := arr.ValueAt(i)
		counts[v]++
		if !hasNeg && v < 0 {
			hasNeg = true
		}
		if v != prev {
			runs++
			prev = v
		}
	}

	var (
		topVal   T
		topCount uint64
	)
	for v, c := range counts {
		if c > topCount {
			topVal = v
			topCount = c
		}
	}

	return signedIntStats[T]{
		baseStats: baseStats[T]{
			isConst:       len(counts) == 1 && topCount == n,
			distinctRatio: float64(len(counts)) / float64(n),
			avgRunLength:  float64(n) / float64(runs),
			topValue:      topVal,
			topCount:      topCount,
		},
		hasNegative: hasNeg,
	}
}

// computeFloatStats scans the array once to compute base stats using
// bitwise comparison for run boundaries (correct for NaN).
func computeFloatStats[T Float](arr array.Array[T]) baseStats[T] {
	n := arr.Length()
	if n == 0 {
		return baseStats[T]{}
	}

	var (
		counts = make(map[T]uint64, 256)
		runs   = uint64(1)
		prev   = arr.ValueAt(0)
	)
	counts[prev]++

	for i := uint64(1); i < n; i++ {
		v := arr.ValueAt(i)
		counts[v]++
		if !cmpFloats(v, prev) {
			runs++
			prev = v
		}
	}

	var (
		topVal   T
		topCount uint64
	)
	for v, c := range counts {
		if c > topCount {
			topVal = v
			topCount = c
		}
	}

	return baseStats[T]{
		isConst:       len(counts) == 1 && topCount == n,
		distinctRatio: float64(len(counts)) / float64(n),
		avgRunLength:  float64(n) / float64(runs),
		topValue:      topVal,
		topCount:      topCount,
	}
}

// computeStringStats scans the array once to compute base stats for strings.
func computeStringStats(arr array.Array[string]) baseStats[string] {
	n := arr.Length()
	if n == 0 {
		return baseStats[string]{}
	}

	var (
		counts = make(map[string]uint64, 256)
		runs   = uint64(1)
		prev   = arr.ValueAt(0)
	)
	counts[prev]++

	for i := uint64(1); i < n; i++ {
		v := arr.ValueAt(i)
		counts[v]++
		if v != prev {
			runs++
			prev = v
		}
	}

	var (
		topVal   string
		topCount uint64
	)
	for v, c := range counts {
		if c > topCount {
			topVal = v
			topCount = c
		}
	}

	return baseStats[string]{
		isConst:       len(counts) == 1 && topCount == n,
		distinctRatio: float64(len(counts)) / float64(n),
		avgRunLength:  float64(n) / float64(runs),
		topValue:      topVal,
		topCount:      topCount,
	}
}
