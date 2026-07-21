package btrblocks

import "github.com/axiomhq/btrblocks/array"

// intStats extends baseStats with min/max bounds, sequence detection, and
// negative-value tracking for integer arrays. hasNegative is constant-false
// for unsigned instantiations.
type intStats[T array.Integer] struct {
	baseStats[T]
	distinct     map[T]uint64 // value → frequency, used to bound dictionary planning
	hasNegative  bool
	isSequence   bool
	min          T
	max          T
	sequenceBase T
	sequenceStep T
}

// computeIntStatsForPlanner scans the full column once and keeps min/max,
// sequence, run-length, and negative-value statistics. It retains bounded
// frequencies only when an eligible scheme needs them.
func computeIntStatsForPlanner[T array.Integer](arr array.Array[T], collectFrequencies bool) intStats[T] {
	n := arr.Length()
	if n == 0 {
		return intStats[T]{
			baseStats: baseStats[T]{src: arr},
		}
	}
	runs := uint64(1)
	prev := arr.ValueAt(0)
	sequenceBase := prev
	var sequenceStep T
	isSequence := n >= 2
	if isSequence {
		sequenceStep = arr.ValueAt(1) - prev
		isSequence = sequenceStep != 0
	}
	hasNegative := prev < 0
	minValue := prev
	maxValue := prev
	mostFrequent := uint64(1)

	// Discriminators, bools, and other uint8 metadata have a fixed domain.
	// Count them by value and materialize the usually tiny distinct map once,
	// avoiding a hash lookup for every row. Wider integers retain the bounded
	// map policy below.
	useByteCounts := collectFrequencies && array.PTypeOfPrimitive[T]() == array.PTypeUint8
	var byteCounts [256]uint64
	var distinct map[T]uint64
	if useByteCounts {
		byteCounts[uint8(prev)] = 1
	} else if collectFrequencies {
		// Avoid reserving a full page-sized dictionary for small metadata arrays.
		distinct = make(map[T]uint64, distinctInitialCapacity(n))
		distinct[prev] = 1
	}

	for i := uint64(1); i < n; i++ {
		v := arr.ValueAt(i)
		if isSequence && v-prev != sequenceStep {
			isSequence = false
		}
		if useByteCounts {
			idx := uint8(v)
			byteCounts[idx]++
			mostFrequent = max(mostFrequent, byteCounts[idx])
		} else if distinct != nil {
			if count, exists := distinct[v]; exists {
				count++
				distinct[v] = count
				mostFrequent = max(mostFrequent, count)
			} else {
				if len(distinct) >= maxRetainedDistinctValues || uint64(len(distinct)) >= n/2 {
					// Stop paying hash-table costs during generic stats collection.
					// The planner samples larger dictionaries and rebuilds the exact
					// map only when dictionary encoding wins.
					distinct = nil
				} else {
					distinct[v] = 1
				}
			}
		}
		if !hasNegative && v < 0 {
			hasNegative = true
		}
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

	var dc uint64
	if !collectFrequencies {
		dc = n
		mostFrequent = 0
	} else if useByteCounts {
		for _, count := range byteCounts {
			if count > 0 {
				dc++
			}
		}
		distinct = make(map[T]uint64, dc)
		for value, count := range byteCounts {
			if count > 0 {
				distinct[T(value)] = count
			}
		}
	} else {
		// When distinct is nil, cardinality exceeded the retained-map budget.
		// Report n so analytical users reject it; dictionary planning follows
		// the nil map into the bounded sample estimator instead.
		dc = uint64(len(distinct))
		if distinct == nil {
			dc = n
			mostFrequent = 0
		}
	}
	return intStats[T]{
		baseStats: baseStats[T]{
			src:           arr,
			isConst:       runs == 1,
			distinctCount: dc,
			avgRunLength:  float64(n) / float64(runs),
			mostFrequent:  mostFrequent,
		},
		distinct:     distinct,
		hasNegative:  hasNegative,
		isSequence:   isSequence,
		min:          minValue,
		max:          maxValue,
		sequenceBase: sequenceBase,
		sequenceStep: sequenceStep,
	}
}
