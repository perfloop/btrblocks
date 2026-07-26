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
	var frequencyCutoff uint64
	if collectFrequencies {
		frequencyCutoff = n/2 + n%2
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
			} else if uint64(len(distinct)) >= frequencyCutoff {
				// A new key after ceil(n/2) retained keys makes Dict ineligible and leaves
				// at most floor(n/2) occurrences for any key, below Sparse's 90% threshold.
				distinctOverflow = true
			} else {
				distinct[v] = 1
				if len(distinct) > maxRetainedStringDistinctValues {
					// The fixed cap remains a conservative retained-frequency limit, not
					// evidence that either planner is ineligible.
					distinctOverflow = true
				}
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
