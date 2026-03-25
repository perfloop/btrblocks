package btrblocks

import (
	"math"

	"github.com/axiomhq/btrblocks/array"
)

func floatKey[T Float](value T) uint64 {
	switch v := any(value).(type) {
	case float32:
		return uint64(math.Float32bits(v))
	case float64:
		return math.Float64bits(v)
	default:
		return 0
	}
}

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
	distinct map[uint64]uint64 // floatKey(value) → ordinal index, used by dict build
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
		return floatStats[T]{base: baseStats[T]{src: arr}}
	}

	distinct := make(map[uint64]uint64, 256)
	runs := uint64(1)
	prev := arr.ValueAt(0)
	distinct[floatKey(prev)] = 0

	for i := uint64(1); i < n; i++ {
		v := arr.ValueAt(i)
		key := floatKey(v)
		if _, exists := distinct[key]; !exists {
			distinct[key] = uint64(len(distinct))
		}
		if !cmpFloatRuns(v, prev) {
			runs++
			prev = v
		}
	}

	return floatStats[T]{
		base: baseStats[T]{
			src:            arr,
			isConst:        len(distinct) == 1,
			distinctCount:  uint64(len(distinct)),
			distinctRatio:  float64(len(distinct)) / float64(n),
			avgRunLength:   float64(n) / float64(runs),
		},
		distinct: distinct,
	}
}
