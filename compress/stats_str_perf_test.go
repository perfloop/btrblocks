package compress

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

const stringStatsBenchmarkRows = 4096

// TestStringStatsFrequencyBudgetPreservesPlannerEligibility checks the two
// cardinality boundaries where a retained frequency map can affect dictionary
// and sparse planning. The statistics scan may stop retaining frequencies once
// either consumer is provably ineligible, but its other planning signals must
// remain unchanged.
func TestStringStatsFrequencyBudgetPreservesPlannerEligibility(t *testing.T) {
	const rows = stringStatsBenchmarkRows

	cases := []struct {
		name             string
		distinct         int
		wantDictEstimate estimateType
	}{
		{name: "at-half", distinct: rows / 2, wantDictEstimate: estimateSample},
		{name: "above-half", distinct: rows/2 + 1, wantDictEstimate: estimateSkip},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			values := stringStatsValues(rows, tc.distinct)
			var wantTotalBytes uint64
			for _, value := range values {
				wantTotalBytes += uint64(len(value))
			}

			stats := computeStringStatsForPlanner(mustStrings(t, values), true)
			require.False(t, stats.isConst)
			require.Equal(t, wantTotalBytes, stats.totalBytes)
			require.Equal(t, 1.0, stats.avgRunLength)
			if tc.distinct == rows/2 {
				require.Equal(t, uint64(tc.distinct), stats.estimatedDistinctCount)
			} else {
				require.Greater(t, stats.estimatedDistinctCount, uint64(rows/2))
			}

			ctx := newPlanContext(Options{})
			dictEstimate := estimateStringDict(stats, ctx, stats.estimatedDistinctCount)
			sparseEstimate := estimateSparseGeneric(stats, ctx, stringCanSample(stats.Source(), ctx))
			require.Equal(t, tc.wantDictEstimate, dictEstimate.kind)
			require.Equal(t, estimateSkip, sparseEstimate.kind)
		})
	}
}

func BenchmarkComputeStringStatsAllUnique(b *testing.B) {
	for _, rows := range []int{1 << 10, stringStatsBenchmarkRows, 1 << 13} {
		b.Run(fmt.Sprintf("rows=%d", rows), func(b *testing.B) {
			benchmarkComputeStringStats(b, stringStatsValues(rows, rows))
		})
	}
}

// BenchmarkComputeStringStatsDictionaryEligible guards the common retained-map
// path: the budget check must not add measurable planner overhead while Dict is
// still eligible.
func BenchmarkComputeStringStatsDictionaryEligible(b *testing.B) {
	benchmarkComputeStringStats(b, stringStatsValues(stringStatsBenchmarkRows, 64))
}

func benchmarkComputeStringStats(b *testing.B, values []string) {
	b.Helper()
	arr := mustStrings(b, values)
	var stats stringStats
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		stats = computeStringStatsForPlanner(arr, true)
	}
	b.StopTimer()

	if stats.estimatedDistinctCount == 0 || stats.totalBytes == 0 {
		b.Fatalf("invalid stats: distinct=%d totalBytes=%d", stats.estimatedDistinctCount, stats.totalBytes)
	}
}

func stringStatsValues(rows, distinct int) []string {
	values := make([]string, rows)
	for i := range values {
		values[i] = fmt.Sprintf("planner-string-%05d", i%distinct)
	}
	return values
}
