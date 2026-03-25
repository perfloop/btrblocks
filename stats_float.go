package btrblocks

import (
	"math"

	"github.com/axiomhq/btrblocks/array"
)


func floatFromBits[T Float](bits uint64) T {
	var zero T
	switch any(zero).(type) {
	case float32:
		return T(math.Float32frombits(uint32(bits)))
	case float64:
		return T(math.Float64frombits(bits))
	default:
		return zero
	}
}


// floatStats extends baseStats with pre-computed distinct values for float arrays.
type floatStats[T Float] struct {
	base     baseStats[T]
	distinct map[uint64]uint64 // floatBits(value) → ordinal index, used by dict build
}

func (s floatStats[T]) Source() array.Array[T]                { return s.base.Source() }
func (s floatStats[T]) Sample(ctx planContext) array.ArrayCore[T] { return s.base.Sample(ctx) }

// computeFloatStats keeps exact distinct counts using bit-pattern equality so
// dict/const decisions remain bit-exact, but run detection follows normal float
// equality to match the Rust planner's RLE heuristic.
// The full distinct map is retained for dict construction (see stats_uint.go).
func computeFloatStats[T Float](arr array.Array[T]) floatStats[T] {
	n := arr.Length()
	if n == 0 {
		return floatStats[T]{base: baseStats[T]{src: arr, cached: arr}}
	}

	distinct := make(map[uint64]uint64, 256)
	runs := uint64(1)
	prev := arr.ValueAt(0)
	distinct[floatBits(prev)] = 0

	for i := uint64(1); i < n; i++ {
		v := arr.ValueAt(i)
		if distinct != nil {
			key := floatBits(v)
			if _, exists := distinct[key]; !exists {
				if uint64(len(distinct)) >= n/2 {
					// Dict encoding is not viable — more than n/2 distinct values.
					// Nil the map to bound stats memory for high-cardinality columns.
					// Safe because the dict estimator rejects at this threshold, so
					// the dict build function is never called with a nil map.
					distinct = nil
				} else {
					distinct[key] = uint64(len(distinct))
				}
			}
		}
		if !cmpFloatBits(v, prev) {
			runs++
			prev = v
		}
	}

	// When distinct is nil, the true count exceeded n/2. Report n so downstream
	// estimators (dict, const) see a value that triggers their rejection guards.
	dc := uint64(len(distinct))
	if distinct == nil {
		dc = n
	}

	return floatStats[T]{
		base: baseStats[T]{
			src:           arr,
			cached:        sampleArray(arr),
			isConst:       runs == 1,
			distinctCount: dc,
			avgRunLength:  float64(n) / float64(runs),
		},
		distinct: distinct,
	}
}
