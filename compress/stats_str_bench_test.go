package compress

import (
	"fmt"
	"testing"
)

// BenchmarkStringStatsFrequencyCutoff exercises the two string-frequency
// planner shapes covered by the retained-frequency cutoff.
func BenchmarkStringStatsFrequencyCutoff(b *testing.B) {
	b.Run("all-unique/rows=4096", func(b *testing.B) {
		benchmarkStringStatsFrequencyCutoff(b, nativeStringStatsValues(4096, 4096))
	})
	b.Run("dictionary-eligible/rows=4096", func(b *testing.B) {
		benchmarkStringStatsFrequencyCutoff(b, nativeStringStatsValues(4096, 64))
	})
}

func benchmarkStringStatsFrequencyCutoff(b *testing.B, values []string) {
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

func nativeStringStatsValues(rows, distinct int) []string {
	values := make([]string, rows)
	for i := range values {
		values[i] = fmt.Sprintf("planner-string-%05d", i%distinct)
	}
	return values
}
