package btrblocks

import (
	"math"

	"github.com/axiomhq/btrblocks/array"
)

const (
	distinctRatioThreshold = 0.5
)

// baseStats holds type-agnostic planner statistics for one array.
type baseStats[T Integer | Float | String] struct {
	src           array.Array[T]
	isConst       bool
	distinctCount uint64
	distinctRatio float64
	avgRunLength  float64
	topValue      T
	topCount      uint64
}

// unsignedStats extends baseStats with min/max bounds for unsigned arrays.
type unsignedStats[T UnsignedInteger] struct {
	base baseStats[T]
	min  T
	max  T
}

// signedStats extends baseStats with negative-value tracking for signed arrays.
type signedStats[T SignedInteger] struct {
	base        baseStats[T]
	hasNegative bool
}

// stringDistinctKey mirrors the Rust reference's cheap string-cardinality estimate:
// strings are grouped by byte length and the first 8 bytes of content instead of
// hashing the full string payload.
type stringDistinctKey struct {
	length uint64
	prefix [8]byte
}

func (s baseStats[T]) Source() array.Array[T] {
	return s.src
}

func (s baseStats[T]) Sample(ctx planContext) array.Array[T] {
	if ctx.isSample {
		return s.src
	}
	return sampleArray(s.src)
}

func (s unsignedStats[T]) Source() array.Array[T] {
	return s.base.Source()
}

func (s unsignedStats[T]) Sample(ctx planContext) array.Array[T] {
	return s.base.Sample(ctx)
}

func (s signedStats[T]) Source() array.Array[T] {
	return s.base.Source()
}

func (s signedStats[T]) Sample(ctx planContext) array.Array[T] {
	return s.base.Sample(ctx)
}

// computeUnsignedStats scans the full column once and keeps exact numeric
// cardinality, min/max, top value, and run-length statistics. Dict planning for
// integers uses exact distinct counts in the Rust reference, so we keep this
// path exact as well.
func computeUnsignedStats[T UnsignedInteger](arr array.Array[T]) unsignedStats[T] {
	n := arr.Length()
	if n == 0 {
		return unsignedStats[T]{
			base: baseStats[T]{
				src: arr,
			},
		}
	}

	counts := make(map[T]uint64, 256)
	runs := uint64(1)
	prev := arr.ValueAt(0)
	minValue := prev
	maxValue := prev
	counts[prev]++

	for i := uint64(1); i < n; i++ {
		v := arr.ValueAt(i)
		counts[v]++
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

	var topValue T
	var topCount uint64
	for v, count := range counts {
		if count > topCount {
			topValue = v
			topCount = count
		}
	}

	return unsignedStats[T]{
		base: baseStats[T]{
			src:           arr,
			isConst:       len(counts) == 1 && topCount == n,
			distinctCount: uint64(len(counts)),
			distinctRatio: float64(len(counts)) / float64(n),
			avgRunLength:  float64(n) / float64(runs),
			topValue:      topValue,
			topCount:      topCount,
		},
		min: minValue,
		max: maxValue,
	}
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

// computeFloatStats keeps exact distinct counts using bit-pattern equality so
// NaNs and signed zero follow codec semantics rather than Go's == behavior.
func computeFloatStats[T Float](arr array.Array[T]) baseStats[T] {
	n := arr.Length()
	if n == 0 {
		return baseStats[T]{src: arr}
	}

	type entry struct {
		value T
		count uint64
	}

	counts := make(map[uint64]entry, 256)
	runs := uint64(1)
	prev := arr.ValueAt(0)
	firstKey := floatKey(prev)
	counts[firstKey] = entry{value: prev, count: 1}

	for i := uint64(1); i < n; i++ {
		v := arr.ValueAt(i)
		key := floatKey(v)
		item := counts[key]
		item.value = v
		item.count++
		counts[key] = item
		if !cmpFloats(v, prev) {
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

	return baseStats[T]{
		src:           arr,
		isConst:       len(counts) == 1 && topCount == n,
		distinctCount: uint64(len(counts)),
		distinctRatio: float64(len(counts)) / float64(n),
		avgRunLength:  float64(n) / float64(runs),
		topValue:      topValue,
		topCount:      topCount,
	}
}

// computeStringStats keeps constant and run statistics exact, but uses an
// approximate distinct estimate that only considers length plus the first 8
// bytes. This matches the Rust reference more closely than exact string hashing
// and avoids full-payload work during planner stats generation.
func computeStringStats(arr array.Array[string]) baseStats[string] {
	n := arr.Length()
	if n == 0 {
		return baseStats[string]{src: arr}
	}

	distinct := make(map[stringDistinctKey]struct{}, 256)
	runs := uint64(1)
	prev := arr.ValueAt(0)
	distinct[stringKey(prev)] = struct{}{}
	isConst := true

	for i := uint64(1); i < n; i++ {
		v := arr.ValueAt(i)
		distinct[stringKey(v)] = struct{}{}
		if v != prev {
			isConst = false
			runs++
			prev = v
		}
	}

	topValue := ""
	topCount := uint64(0)
	if isConst {
		topValue = arr.ValueAt(0)
		topCount = n
	}

	return baseStats[string]{
		src:           arr,
		isConst:       isConst,
		distinctCount: uint64(len(distinct)),
		distinctRatio: float64(len(distinct)) / float64(n),
		avgRunLength:  float64(n) / float64(runs),
		topValue:      topValue,
		topCount:      topCount,
	}
}

func stringKey(value string) stringDistinctKey {
	key := stringDistinctKey{length: uint64(len(value))}
	for i := 0; i < len(key.prefix) && i < len(value); i++ {
		key.prefix[i] = value[i]
	}
	return key
}
