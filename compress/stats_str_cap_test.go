package compress

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestStringStatsFrequencyRetentionCap checks only the retained-frequency
// state at the exact cap boundary. Unlike the half-cardinality cutoff, cap
// overflow is conservative and does not establish planner eligibility itself.
func TestStringStatsFrequencyRetentionCap(t *testing.T) {
	// Keep n/2 above the cap so the cases exercise the retention policy alone.
	const rows = 3 * maxRetainedStringDistinctValues
	cases := []struct {
		name              string
		distinct          int
		wantDistinctCount uint64
		wantMostFrequent  uint64
	}{
		{name: "at-4096", distinct: maxRetainedStringDistinctValues, wantDistinctCount: maxRetainedStringDistinctValues, wantMostFrequent: 3},
		{name: "at-4097", distinct: maxRetainedStringDistinctValues + 1, wantDistinctCount: rows, wantMostFrequent: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stats := computeStringStatsForPlanner(mustStrings(t, stringStatsCapValues(rows, tc.distinct)), true)
			require.Equal(t, tc.wantDistinctCount, stats.estimatedDistinctCount)
			require.Equal(t, tc.wantMostFrequent, stats.MostFrequentCount())
		})
	}
}

func stringStatsCapValues(rows, distinct int) []string {
	values := make([]string, rows)
	for i := range values {
		values[i] = fmt.Sprintf("stats-cap-%05d", i%distinct)
	}
	return values
}
