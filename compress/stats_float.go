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
	prev := arr.ValueAt(0)
	if !collectFrequencies {
		for i := uint64(1); i < n; i++ {
			v := arr.ValueAt(i)
			if !array.CmpFloatBits(v, prev) {
				runs++
				prev = v
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
		return computeFloatStatsWithEagerSketch(arr)
	}

	// Avoid reserving a full page-sized dictionary for small metadata arrays.
	distinct := make(map[uint64]uint64, distinctInitialCapacity(n))
	distinct[array.FloatBits(prev)] = 1
	mostFrequent := uint64(1)
	for i := uint64(1); i < n; i++ {
		v := arr.ValueAt(i)
		key := array.FloatBits(v)
		if count, exists := distinct[key]; exists {
			count++
			distinct[key] = count
			mostFrequent = max(mostFrequent, count)
		} else if len(distinct) >= maxRetainedDistinctValues || uint64(len(distinct)) >= n/2 {
			dc, runs := collectFloatStatsOverflow(arr, i, prev, runs, distinct)
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
		}
		if !array.CmpFloatBits(v, prev) {
			runs++
			prev = v
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

// computeFloatStatsWithEagerSketch avoids map seeding when the half-length
// retention boundary can be reached before the fixed retained-map capacity.
func computeFloatStatsWithEagerSketch[T array.Float](arr array.Array[T]) floatStats[T] {
	n := arr.Length()
	distinct := make(map[uint64]uint64, distinctInitialCapacity(n))
	var distinctBits [256]uint64
	runs := uint64(1)
	prev := arr.ValueAt(0)
	distinct[array.FloatBits(prev)] = 1
	mostFrequent := uint64(1)

	for i := range n {
		v := arr.ValueAt(i)
		hash := mixFloatBits(array.FloatBits(v))
		bucket := hash & (floatDistinctSketchBuckets - 1)
		distinctBits[bucket/64] |= uint64(1) << (bucket & 63)
		if i == 0 {
			continue
		}
		if distinct != nil {
			key := array.FloatBits(v)
			if count, exists := distinct[key]; exists {
				count++
				distinct[key] = count
				mostFrequent = max(mostFrequent, count)
			} else if len(distinct) >= maxRetainedDistinctValues || uint64(len(distinct)) >= n/2 {
				distinct = nil
			} else {
				distinct[key] = 1
			}
		}
		if !array.CmpFloatBits(v, prev) {
			runs++
			prev = v
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

// collectFloatStatsOverflow seeds the sketch from the retained exact keys,
// then scans the triggering value and remaining suffix without map-mode work.
func collectFloatStatsOverflow[T array.Float](arr array.Array[T], start uint64, prev T, runs uint64, distinct map[uint64]uint64) (uint64, uint64) {
	n := arr.Length()
	var distinctBits [256]uint64
	for key := range distinct {
		hash := mixFloatBits(key)
		bucket := hash & (floatDistinctSketchBuckets - 1)
		distinctBits[bucket/64] |= uint64(1) << (bucket & 63)
	}
	for i := start; i < n; i++ {
		v := arr.ValueAt(i)
		hash := mixFloatBits(array.FloatBits(v))
		bucket := hash & (floatDistinctSketchBuckets - 1)
		distinctBits[bucket/64] |= uint64(1) << (bucket & 63)
		if !array.CmpFloatBits(v, prev) {
			runs++
			prev = v
		}
	}
	return estimateFloatDistinct(distinctBits, n), runs
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
