package btrblocks

import (
	"math/bits"

	"github.com/axiomhq/btrblocks/array"
)

// arrayStats holds statistics computed from the full array for
// stats-based builder rejection, matching Vortex's gen_stats approach.
type arrayStats[T Integer | Float | String] struct {
	distinctRatio float64 // distinctCount / length
	avgRunLength  float64 // length / runCount
	hasNegative   bool    // true if any signed integer value < 0
	minUint64     uint64  // min value as uint64 (unsigned integers only)
	maxUint64     uint64  // max value as uint64 (unsigned integers only)
	topValue      T       // most frequent value
	topCount      uint64  // occurrence count of topValue
}

// shouldSkip returns true if a builder of the given kind should be skipped
// based on array statistics.
func (h arrayStats[T]) shouldSkip(kind CodecType, n uint64) bool {
	switch kind {
	case CodecTypeDict:
		// Vortex: skip if >50% distinct values.
		return h.distinctRatio > 0.5
	case CodecTypeRunend:
		// Vortex: skip if average run length < 4.
		return h.avgRunLength < 4.0
	case CodecTypeZigzag:
		// Zigzag only helps when there are negative values to fold.
		return !h.hasNegative
	case CodecTypeFoR:
		// FoR only helps when subtracting min reduces the bit width.
		return bits.Len64(h.maxUint64-h.minUint64) >= bits.Len64(h.maxUint64)
	case CodecTypeSparse:
		// Sparse only helps when one value dominates ≥90% of the array.
		return n == 0 || float64(h.topCount)/float64(n) < 0.9
	default:
		return false
	}
}

// computeArrayStats scans the full array once to compute stats
// used for O(1) codec rejection, matching Vortex's gen_stats approach.
func computeArrayStats[T Integer | Float | String](arr array.Array[T]) arrayStats[T] {
	n := arr.Length()
	if n == 0 {
		return arrayStats[T]{}
	}

	var (
		counts = make(map[T]uint64, 256)
		runs   = uint64(1)
		hasNeg = false
		minU   = ^uint64(0)
		maxU   = uint64(0)
		prev   = arr.ValueAt(0)
	)

	counts[prev]++
	hasNeg = isNegative(prev)
	if u, ok := toUint64(prev); ok {
		minU, maxU = u, u
	}

	for i := uint64(1); i < n; i++ {
		v := arr.ValueAt(i)
		counts[v]++
		if !hasNeg {
			hasNeg = isNegative(v)
		}
		if u, ok := toUint64(v); ok {
			if u < minU {
				minU = u
			}
			if u > maxU {
				maxU = u
			}
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

	return arrayStats[T]{
		distinctRatio: float64(len(counts)) / float64(n),
		avgRunLength:  float64(n) / float64(runs),
		hasNegative:   hasNeg,
		minUint64:     minU,
		maxUint64:     maxU,
		topValue:      topVal,
		topCount:      topCount,
	}
}

// toUint64 converts unsigned integer values to uint64 for min/max tracking.
func toUint64(v any) (uint64, bool) {
	switch val := v.(type) {
	case uint8:
		return uint64(val), true
	case uint16:
		return uint64(val), true
	case uint32:
		return uint64(val), true
	case uint64:
		return val, true
	default:
		return 0, false
	}
}

// isNegative returns true if v is a negative signed integer.
func isNegative(v any) bool {
	switch val := v.(type) {
	case int8:
		return val < 0
	case int16:
		return val < 0
	case int32:
		return val < 0
	case int64:
		return val < 0
	}
	return false
}
