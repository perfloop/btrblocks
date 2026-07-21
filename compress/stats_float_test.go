package compress

import (
	"math"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

func TestComputeFloatStatsTreatsNaNsAsSameRun(t *testing.T) {
	nan := math.Float64frombits(0x7ff8000000000001)

	stats := computeFloatStatsForPlanner(array.NewPrimitivesUnsafe([]float64{nan, nan}), true)
	require.Equal(t, uint64(1), stats.distinctCount)
	require.Equal(t, 2.0, stats.avgRunLength)
}

func TestComputeFloatStatsTreatsSignedZeroAsDifferentRuns(t *testing.T) {
	values := []float64{math.Copysign(0, 1), math.Copysign(0, -1)}

	stats := computeFloatStatsForPlanner(array.NewPrimitivesUnsafe(values), true)
	require.Equal(t, uint64(2), stats.distinctCount)
	require.Equal(t, 1.0, stats.avgRunLength)
}

func TestComputeFloatStatsEstimatesCardinalityAfterMapLimit(t *testing.T) {
	for _, test := range []struct {
		name     string
		distinct int
	}{
		{name: "low", distinct: 1024},
		{name: "high", distinct: 65536},
	} {
		t.Run(test.name, func(t *testing.T) {
			values := make([]float64, 65536)
			for i := range values {
				values[i] = float64(i % test.distinct)
			}
			stats := computeFloatStatsForPlanner(array.NewPrimitivesUnsafe(values), true)
			if diff := math.Abs(float64(stats.distinctEstimate) - float64(test.distinct)); diff > float64(test.distinct)/10 {
				t.Fatalf("distinctEstimate = %d, want %d +- %v", stats.distinctEstimate, test.distinct, float64(test.distinct)/10)
			}
		})
	}
}
