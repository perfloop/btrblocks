package compress

import (
	"fmt"
	"math"
	"testing"

	"github.com/axiomhq/btrblocks/codec"
	"github.com/stretchr/testify/require"
)

func TestComputeStringStatsDistinguishesCommonPrefixes(t *testing.T) {
	values := []string{"abcdefghX", "abcdefghY"}

	stats := computeStringStatsForPlanner(mustStrings(t, values), true)
	require.Equal(t, uint64(2), stats.estimatedDistinctCount)
}

func TestStringStatsFrequencyOddHalfCutoff(t *testing.T) {
	const rows = 4095
	const prefixDistinct = rows/2 + rows%2

	values := make([]string, rows)
	values[0] = "dominant"
	for i := 1; i < prefixDistinct; i++ {
		values[i] = fmt.Sprintf("odd-prefix-%04d", i)
	}
	for i := prefixDistinct; i < len(values); i++ {
		values[i] = values[0]
	}

	stats := computeStringStatsForPlanner(mustStrings(t, values), true)
	require.Equal(t, uint64(prefixDistinct), stats.estimatedDistinctCount)
	require.Equal(t, uint64(prefixDistinct), stats.MostFrequentCount())
	require.False(t, countDominates(uint64(rows), uint64(prefixDistinct)))

	ctx := newPlanContext(Options{})
	require.Equal(t, estimateSkip, estimateStringDict(stats, ctx, stats.estimatedDistinctCount).kind)
	require.True(t, stringCanSample(stats.Source(), ctx))
	require.Equal(t, estimateSkip, estimateSparseGeneric(stats, ctx, true).kind)

	encoded, err := StringArray(mustStrings(t, values), Options{})
	require.NoError(t, err)
	require.Equal(t, codec.CodecTypeRaw, encoded.CodecType())
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
