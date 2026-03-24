package btrblocks

import (
	"math"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

func TestComputeStringStatsUsesPrefixDistinctEstimate(t *testing.T) {
	values := []string{"abcdefghX", "abcdefghY"}

	stats := computeStringStats(array.NewStrings(values))
	require.Equal(t, uint64(1), stats.estimatedDistinctCount)
}

func TestComputeFloatStatsTreatsNaNsAsRunBreaks(t *testing.T) {
	nan := math.Float64frombits(0x7ff8000000000001)

	stats := computeFloatStats(array.NewPrimitivesUnsafe([]float64{nan, nan}))
	require.Equal(t, uint64(1), stats.base.distinctCount)
	require.Equal(t, 1.0, stats.base.avgRunLength)
}

func TestComputeFloatStatsTreatsSignedZeroAsOneRun(t *testing.T) {
	values := []float64{math.Copysign(0, 1), math.Copysign(0, -1)}

	stats := computeFloatStats(array.NewPrimitivesUnsafe(values))
	require.Equal(t, uint64(2), stats.base.distinctCount)
	require.Equal(t, 2.0, stats.base.avgRunLength)
}

func TestComputeFloatStatsTracksNonFiniteRatio(t *testing.T) {
	values := []float64{
		1,
		math.Inf(1),
		math.NaN(),
		2,
	}

	stats := computeFloatStats(array.NewPrimitivesUnsafe(values))
	require.Equal(t, 0.5, stats.base.nonFiniteRatio)
}
