package compress

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

	runs := uint64(1)
	prevBits := array.FloatBits(arr.ValueAt(0))
	if !collectFrequencies {
		for i := uint64(1); i < n; i++ {
			key := array.FloatBits(arr.ValueAt(i))
			if key != prevBits {
				runs++
				prevBits = key
			}
		}
		return floatStats[T]{
			baseStats: baseStats[T]{
				src:           arr,
				isConst:       runs == 1,
				distinctCount: n,
				avgRunLength:  float64(n) / float64(runs),
			},
			distinctEstimate: n,
		}
	}
	if n/2 <= maxRetainedDistinctValues {
		// The half-length limit can trigger no later than the fixed map cap.
		// Keep the one-pass sketch here instead of reconstructing it from map keys.
		distinct := make(map[uint64]uint64, distinctInitialCapacity(n))
		var distinctBits [256]uint64
		distinct[prevBits] = 1
		mostFrequent := uint64(1)
		for i := range n {
			key := array.FloatBits(arr.ValueAt(i))
			hash := mixFloatBits(key)
			bucket := hash & (floatDistinctSketchBuckets - 1)
			distinctBits[bucket/64] |= uint64(1) << (bucket & 63)
			if i == 0 {
				continue
			}
			if distinct != nil {
				if count, exists := distinct[key]; exists {
					count++
					distinct[key] = count
					mostFrequent = max(mostFrequent, count)
				} else if uint64(len(distinct)) >= n/2 {
					distinct = nil
				} else {
					distinct[key] = 1
				}
			}
			if key != prevBits {
				runs++
				prevBits = key
			}
		}
		dc := uint64(len(distinct))
		if distinct == nil {
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

	// Avoid reserving a full page-sized dictionary for small metadata arrays.
	distinct := make(map[uint64]uint64, distinctInitialCapacity(n))
	var distinctBits [256]uint64
	distinct[prevBits] = 1
	setFloatDistinctBit(&distinctBits, prevBits)
	mostFrequent := uint64(1)
	for i := uint64(1); i < n; i++ {
		key := array.FloatBits(arr.ValueAt(i))
		if count, exists := distinct[key]; exists {
			count++
			distinct[key] = count
			mostFrequent = max(mostFrequent, count)
		} else if len(distinct) >= maxRetainedDistinctValues {
			dc, runs := collectFloatStatsOverflow(arr, i, prevBits, runs, &distinctBits)
			return floatStats[T]{
				baseStats: baseStats[T]{
					src:           arr,
					isConst:       runs == 1,
					distinctCount: dc,
					avgRunLength:  float64(n) / float64(runs),
				},
				distinctEstimate: dc,
			}
		} else {
			distinct[key] = 1
			setFloatDistinctBit(&distinctBits, key)
		}
		if key != prevBits {
			runs++
			prevBits = key
		}
	}

	dc := uint64(len(distinct))
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

// collectFloatStatsOverflow scans the triggering value and remaining suffix
// after the staged collector has marked each retained exact key.
func collectFloatStatsOverflow[T array.Float](arr array.Array[T], start, prevBits, runs uint64, distinctBits *[256]uint64) (uint64, uint64) {
	n := arr.Length()
	for i := start; i < n; i++ {
		key := array.FloatBits(arr.ValueAt(i))
		setFloatDistinctBit(distinctBits, key)
		if key != prevBits {
			runs++
			prevBits = key
		}
	}
	return estimateFloatDistinct(*distinctBits, n), runs
}

func mixFloatBits(value uint64) uint64 {
	value += 0x9e3779b97f4a7c15
	value = (value ^ value>>30) * 0xbf58476d1ce4e5b9
	value = (value ^ value>>27) * 0x94d049bb133111eb
	return value ^ value>>31
}

func setFloatDistinctBit(bitmap *[256]uint64, key uint64) {
	hash := mixFloatBits(key)
	bucket := hash & (floatDistinctSketchBuckets - 1)
	bitmap[bucket/64] |= uint64(1) << (bucket & 63)
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
