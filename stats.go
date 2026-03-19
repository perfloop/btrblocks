package btrblocks

import (
	"math"

	"github.com/axiomhq/btrblocks/array"
)

type baseStats[T Integer | Float | String] struct {
	isConst       bool
	distinctRatio float64
	avgRunLength  float64
	topValue      T
	topCount      uint64
}

type unsignedStats[T UnsignedInteger] struct {
	base baseStats[T]
	min  T
	max  T
}

type signedStats[T SignedInteger] struct {
	base        baseStats[T]
	hasNegative bool
}

func computeUnsignedStats[T UnsignedInteger](arr array.Array[T]) unsignedStats[T] {
	n := arr.Length()
	if n == 0 {
		return unsignedStats[T]{}
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
			isConst:       len(counts) == 1 && topCount == n,
			distinctRatio: float64(len(counts)) / float64(n),
			avgRunLength:  float64(n) / float64(runs),
			topValue:      topValue,
			topCount:      topCount,
		},
		min: minValue,
		max: maxValue,
	}
}

func computeSignedStats[T SignedInteger](arr array.Array[T]) signedStats[T] {
	n := arr.Length()
	if n == 0 {
		return signedStats[T]{}
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
			isConst:       len(counts) == 1 && topCount == n,
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

func computeFloatStats[T Float](arr array.Array[T]) baseStats[T] {
	n := arr.Length()
	if n == 0 {
		return baseStats[T]{}
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
		isConst:       len(counts) == 1 && topCount == n,
		distinctRatio: float64(len(counts)) / float64(n),
		avgRunLength:  float64(n) / float64(runs),
		topValue:      topValue,
		topCount:      topCount,
	}
}

func computeStringStats(arr array.Array[string]) baseStats[string] {
	n := arr.Length()
	if n == 0 {
		return baseStats[string]{}
	}

	counts := make(map[string]uint64, 256)
	runs := uint64(1)
	prev := arr.ValueAt(0)
	counts[prev]++

	for i := uint64(1); i < n; i++ {
		v := arr.ValueAt(i)
		counts[v]++
		if v != prev {
			runs++
			prev = v
		}
	}

	var topValue string
	var topCount uint64
	for v, count := range counts {
		if count > topCount {
			topValue = v
			topCount = count
		}
	}

	return baseStats[string]{
		isConst:       len(counts) == 1 && topCount == n,
		distinctRatio: float64(len(counts)) / float64(n),
		avgRunLength:  float64(n) / float64(runs),
		topValue:      topValue,
		topCount:      topCount,
	}
}
