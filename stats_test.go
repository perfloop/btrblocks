package btrblocks

import (
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

func TestComputeStringStatsUsesPrefixDistinctEstimate(t *testing.T) {
	values := []string{"abcdefghX", "abcdefghY"}

	stats := computeStringStats(array.NewStrings(values))
	require.False(t, stats.isConst)
	require.Equal(t, uint64(1), stats.distinctCount)
	require.Equal(t, 0.5, stats.distinctRatio)
}

func TestComputeStringStatsKeepsConstDetectionExact(t *testing.T) {
	values := []string{"constant", "constant", "constant"}

	stats := computeStringStats(array.NewStrings(values))
	require.True(t, stats.isConst)
	require.Equal(t, uint64(1), stats.distinctCount)
	require.Equal(t, values[0], stats.topValue)
	require.Equal(t, uint64(len(values)), stats.topCount)
}
