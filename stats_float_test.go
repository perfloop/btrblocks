package btrblocks

import (
	"math"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

func TestComputeFloatStatsTreatsNaNsAsSameRun(t *testing.T) {
	nan := math.Float64frombits(0x7ff8000000000001)

	stats := computeFloatStats(array.NewPrimitivesUnsafe([]float64{nan, nan}))
	require.Equal(t, uint64(1), stats.base.distinctCount)
	require.Equal(t, 2.0, stats.base.avgRunLength)
}

func TestComputeFloatStatsTreatsSignedZeroAsDifferentRuns(t *testing.T) {
	values := []float64{math.Copysign(0, 1), math.Copysign(0, -1)}

	stats := computeFloatStats(array.NewPrimitivesUnsafe(values))
	require.Equal(t, uint64(2), stats.base.distinctCount)
	require.Equal(t, 1.0, stats.base.avgRunLength)
}
