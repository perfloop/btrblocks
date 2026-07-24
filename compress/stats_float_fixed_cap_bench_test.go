package compress

import "testing"

// BenchmarkFloatPlannerFixedCapStats brackets the first staged 256-key
// retention boundary: 256 keys complete the exact map, and 257 keys overflow
// it into the staged sketch collector.
func BenchmarkFloatPlannerFixedCapStats(b *testing.B) {
	shapes := []floatStatsBenchmarkShape{
		{rows: 514, cardinality: 256},
		{rows: 514, cardinality: 257},
	}
	for _, shape := range shapes {
		benchmarkFloat32StatsShape(b, shape)
		benchmarkFloat64StatsShape(b, shape)
	}
}
