package btrblocks

import (
	"math"
	"math/bits"

	"github.com/axiomhq/btrblocks/array"
)

const floatDistinctSketchBuckets = 16384

// floatStats extends baseStats with pre-computed distinct values for float arrays.
type floatStats[T array.Float] struct {
	baseStats[T]
	distinct         map[uint64]uint64 // floatBits(value) → frequency, used by dict planning
	distinctEstimate uint64
}

// computeFloatStatsForPlanner keeps exact distinct counts using bit-pattern
// equality so dict/const decisions remain bit-exact, but run detection follows
// normal float equality to match the Rust planner's RLE heuristic.
// Small distinct maps are retained for dict construction. Larger dictionaries
// are estimated from the shared planner sample and built only if selected.
func computeFloatStatsForPlanner[T array.Float](arr array.Array[T], collectFrequencies bool) floatStats[T] {
	n := arr.Length()
	if n == 0 {
		return floatStats[T]{baseStats: baseStats[T]{src: arr}}
	}
	// Avoid reserving a full page-sized dictionary for small metadata arrays.
	var distinct map[uint64]uint64
	if collectFrequencies {
		distinct = make(map[uint64]uint64, distinctInitialCapacity(n))
	}
	var distinctBits [256]uint64
	runs := uint64(1)
	prev := arr.ValueAt(0)
	if collectFrequencies {
		distinct[array.FloatBits(prev)] = 1
	}
	mostFrequent := uint64(1)

	for i := range n {
		v := arr.ValueAt(i)
		if collectFrequencies {
			hash := mixFloatBits(array.FloatBits(v))
			bucket := hash & (floatDistinctSketchBuckets - 1)
			distinctBits[bucket/64] |= uint64(1) << (bucket & 63)
		}
		if i == 0 {
			continue
		}
		if distinct != nil {
			key := array.FloatBits(v)
			if count, exists := distinct[key]; exists {
				count++
				distinct[key] = count
				mostFrequent = max(mostFrequent, count)
			} else {
				if len(distinct) >= maxRetainedDistinctValues || uint64(len(distinct)) >= n/2 {
					// Stop paying hash-table costs during generic stats collection.
					// The planner samples larger dictionaries and rebuilds the exact
					// map only when dictionary encoding wins.
					distinct = nil
				} else {
					distinct[key] = 1
				}
			}
		}
		if !array.CmpFloatBits(v, prev) {
			runs++
			prev = v
		}
	}

	// When distinct is nil, cardinality exceeded the retained-map budget. Report
	// n so analytical users reject it; dictionary planning follows the nil map
	// into the bounded sample estimator instead.
	dc := uint64(len(distinct))
	if !collectFrequencies {
		dc = n
		mostFrequent = 0
	} else if distinct == nil {
		dc = estimateFloatDistinct(distinctBits, n)
		mostFrequent = 0
	}
	return floatStats[T]{
		baseStats: baseStats[T]{
			src:           arr,
			isConst:       runs == 1,
			distinctCount: dc,
			avgRunLength:  float64(n) / float64(runs),
			mostFrequent:  mostFrequent,
		},
		distinct:         distinct,
		distinctEstimate: dc,
	}
}

func mixFloatBits(value uint64) uint64 {
	value += 0x9e3779b97f4a7c15
	value = (value ^ value>>30) * 0xbf58476d1ce4e5b9
	value = (value ^ value>>27) * 0x94d049bb133111eb
	return value ^ value>>31
}

func estimateFloatDistinct(bitmap [256]uint64, n uint64) uint64 {
	occupied := 0
	for _, word := range bitmap {
		occupied += bits.OnesCount64(word)
	}
	if occupied == floatDistinctSketchBuckets {
		return n
	}
	estimate := -float64(floatDistinctSketchBuckets) * math.Log(float64(floatDistinctSketchBuckets-occupied)/floatDistinctSketchBuckets)
	return min(n, uint64(estimate+0.5))
}
