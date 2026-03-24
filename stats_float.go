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

func isNonFiniteFloat[T Float](value T) bool {
	switch v := any(value).(type) {
	case float32:
		return math.IsNaN(float64(v)) || math.IsInf(float64(v), 0)
	case float64:
		return math.IsNaN(v) || math.IsInf(v, 0)
	default:
		return false
	}
}

// floatDistinctValues maps bit-pattern keys to their canonical float value and
// the ordinal index assigned during stats generation. Dict encoding can reuse
// this map instead of re-scanning the array.
type floatDistinctValues[T Float] struct {
	// byKey maps floatKey(value) → ordinal index.
	byKey map[uint64]uint64
	// values holds the canonical float for each ordinal, in discovery order.
	values []T
}

// floatStats extends baseStats with pre-computed distinct values for float arrays.
type floatStats[T Float] struct {
	base     baseStats[T]
	distinct floatDistinctValues[T]
}

func (s floatStats[T]) Source() array.Array[T]                { return s.base.Source() }
func (s floatStats[T]) Sample(ctx planContext) array.Array[T] { return s.base.Sample(ctx) }

// computeFloatStats keeps exact distinct counts using bit-pattern equality so
// dict/const decisions remain bit-exact, but run detection follows normal float
// equality to match the Rust planner's RLE heuristic.
func computeFloatStats[T Float](arr array.Array[T]) floatStats[T] {
	n := arr.Length()
	if n == 0 {
		return floatStats[T]{base: baseStats[T]{src: arr}}
	}

	type entry struct {
		value   T
		count   uint64
		ordinal uint64
	}

	counts := make(map[uint64]entry, 256)
	var distinctValues []T
	runs := uint64(1)
	prev := arr.ValueAt(0)
	nonFiniteCount := uint64(0)
	if isNonFiniteFloat(prev) {
		nonFiniteCount = 1
	}
	firstKey := floatKey(prev)
	counts[firstKey] = entry{value: prev, count: 1, ordinal: 0}
	distinctValues = append(distinctValues, prev)

	for i := uint64(1); i < n; i++ {
		v := arr.ValueAt(i)
		if isNonFiniteFloat(v) {
			nonFiniteCount++
		}
		key := floatKey(v)
		item, exists := counts[key]
		if !exists {
			item = entry{value: v, ordinal: uint64(len(distinctValues))}
			distinctValues = append(distinctValues, v)
		}
		item.value = v
		item.count++
		counts[key] = item
		if !cmpFloatRuns(v, prev) {
			runs++
			prev = v
		}
	}

	var topValue T
	var topCount uint64
	for _, item := range counts {
		if item.count > topCount {
			topValue = item.value
			topCount = item.count
		}
	}

	byKey := make(map[uint64]uint64, len(counts))
	for key, item := range counts {
		byKey[key] = item.ordinal
	}

	return floatStats[T]{
		base: baseStats[T]{
			src:            arr,
			isConst:        len(counts) == 1 && topCount == n,
			distinctCount:  uint64(len(counts)),
			distinctRatio:  float64(len(counts)) / float64(n),
			nonFiniteRatio: float64(nonFiniteCount) / float64(n),
			avgRunLength:   float64(n) / float64(runs),
			topValue:       topValue,
			topCount:       topCount,
		},
		distinct: floatDistinctValues[T]{
			byKey:  byKey,
			values: distinctValues,
		},
	}
}
