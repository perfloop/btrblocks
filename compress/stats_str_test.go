package compress

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestComputeStringStatsDistinguishesCommonPrefixes(t *testing.T) {
	values := []string{"abcdefghX", "abcdefghY"}

	stats := computeStringStatsForPlanner(mustStrings(t, values), true)
	require.Equal(t, uint64(2), stats.estimatedDistinctCount)
}

// TestStringStatsComputesBaseStats verifies that computeStringStatsForPlanner
// populates isConst and avgRunLength in baseStats, matching the pattern of signed/unsigned/
// float stats. Before the fix, these were not computed — isConst was re-scanned
// in the const estimator and avgRunLength was re-scanned in the RunEnd estimator,
// each adding an O(N) pass per compression.
func TestStringStatsComputesBaseStats(t *testing.T) {
	t.Run("const", func(t *testing.T) {
		arr := mustStrings(t, []string{"x", "x", "x"})
		stats := computeStringStatsForPlanner(arr, true)
		require.True(t, stats.isConst, "all-identical string array must have isConst=true")
		require.Equal(t, 3.0, stats.avgRunLength)
	})

	t.Run("not const", func(t *testing.T) {
		arr := mustStrings(t, []string{"a", "a", "b", "b", "b"})
		stats := computeStringStatsForPlanner(arr, true)
		require.True(t, !stats.isConst)
		if diff := math.Abs(stats.avgRunLength - 2.5); diff > 0.01 {
			t.Fatalf("avgRunLength = %v, want 2.5 +- 0.01", stats.avgRunLength)
		}
	})

	t.Run("single element", func(t *testing.T) {
		arr := mustStrings(t, []string{"z"})
		stats := computeStringStatsForPlanner(arr, true)
		require.True(t, stats.isConst)
		require.Equal(t, 1.0, stats.avgRunLength)
	})

	t.Run("all unique", func(t *testing.T) {
		arr := mustStrings(t, []string{"a", "b", "c", "d"})
		stats := computeStringStatsForPlanner(arr, true)
		require.True(t, !stats.isConst)
		require.Equal(t, 1.0, stats.avgRunLength)
	})
}
