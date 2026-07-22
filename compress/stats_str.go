package compress

import "github.com/axiomhq/btrblocks/array"

// String dictionaries above this cardinality are expensive to retain during
// planning and are rarely competitive with direct FSST encoding on a page.
const maxRetainedStringDistinctValues = 4096

type stringStats struct {
	baseStats[string]
	estimatedDistinctCount uint64
	totalBytes             uint64
}

// computeStringStatsForPlanner counts distinct strings without copying their
// payloads. It also computes isConst and avgRunLength so Schemes does not
// re-scan.
func computeStringStatsForPlanner(arr array.Array[string], collectFrequencies bool) stringStats {
	n := arr.Length()
	if n == 0 {
		return stringStats{baseStats: baseStats[string]{src: arr}}
	}

	var distinct map[string]uint64
	if collectFrequencies {
		distinct = make(map[string]uint64, distinctInitialCapacity(n))
	}
	distinctOverflow := false
	runs := uint64(1)
	isConst := true
	first := arr.ValueAt(0)
	totalBytes := uint64(len(first))
	prev := first

	if collectFrequencies {
		distinct[first] = 1
	}
	mostFrequent := uint64(1)

	for i := uint64(1); i < n; i++ {
		v := arr.ValueAt(i)
		totalBytes += uint64(len(v))
		if collectFrequencies && !distinctOverflow {
			if count, exists := distinct[v]; exists {
				count++
				distinct[v] = count
				mostFrequent = max(mostFrequent, count)
			} else if len(distinct) >= maxRetainedStringDistinctValues || uint64(len(distinct)) >= n/2 {
				// At half cardinality, a new key makes Dict ineligible and precludes
				// Sparse dominance. The fixed cap is a separate conservative retention
				// limit; it does not establish either planner result by itself.
				// Keep the overflow sentinel while continuing the other full-row stats.
				distinct = nil
				distinctOverflow = true
			} else {
				distinct[v] = 1
			}
		}

		if v != prev {
			runs++
			prev = v
			if isConst {
				isConst = false
			}
		}
	}

	distinctCount := uint64(len(distinct))
	if !collectFrequencies {
		distinctCount = n
		mostFrequent = 0
	} else if distinctOverflow {
		distinctCount = n
		mostFrequent = 0
	}
	return stringStats{
		baseStats: baseStats[string]{
			src:           arr,
			isConst:       isConst,
			distinctCount: distinctCount,
			avgRunLength:  float64(n) / float64(runs),
			mostFrequent:  mostFrequent,
		},
		estimatedDistinctCount: distinctCount,
		totalBytes:             totalBytes,
	}
}
