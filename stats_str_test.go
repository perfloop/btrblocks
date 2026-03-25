package btrblocks

import (
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

func TestComputeStringStatsUsesPrefixDistinctEstimate(t *testing.T) {
	values := []string{"abcdefghX", "abcdefghY"}

	stats := computeStringStats(array.NewStrings(values))
	require.Equal(t, uint64(1), stats.estimatedDistinctCount)
}

// TestStringStatsComputesBaseStats verifies that computeStringStats populates
// isConst and avgRunLength in baseStats, matching the pattern of signed/unsigned/
// float stats. Before the fix, these were not computed — isConst was re-scanned
// in the const estimator and avgRunLength was re-scanned in the RunEnd estimator,
// each adding an O(N) pass per compression.
func TestStringStatsComputesBaseStats(t *testing.T) {
	t.Run("const", func(t *testing.T) {
		arr := array.NewStrings([]string{"x", "x", "x"})
		stats := computeStringStats(arr)
		require.True(t, stats.base.isConst, "all-identical string array must have isConst=true")
		require.Equal(t, 3.0, stats.base.avgRunLength)
	})

	t.Run("not const", func(t *testing.T) {
		arr := array.NewStrings([]string{"a", "a", "b", "b", "b"})
		stats := computeStringStats(arr)
		require.False(t, stats.base.isConst)
		require.InDelta(t, 2.5, stats.base.avgRunLength, 0.01)
	})

	t.Run("single element", func(t *testing.T) {
		arr := array.NewStrings([]string{"z"})
		stats := computeStringStats(arr)
		require.True(t, stats.base.isConst)
		require.Equal(t, 1.0, stats.base.avgRunLength)
	})

	t.Run("all unique", func(t *testing.T) {
		arr := array.NewStrings([]string{"a", "b", "c", "d"})
		stats := computeStringStats(arr)
		require.False(t, stats.base.isConst)
		require.Equal(t, 1.0, stats.base.avgRunLength)
	})
}
