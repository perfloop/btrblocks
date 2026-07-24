package compress

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/axiomhq/btrblocks/array"
)

type floatStatsBenchmarkShape struct {
	rows        int
	cardinality int
}

func BenchmarkFloatPlannerStats(b *testing.B) {
	shapes := []floatStatsBenchmarkShape{
		{rows: 64, cardinality: 32},
		{rows: 64, cardinality: 33},
		{rows: 65536, cardinality: 16},
		{rows: 65536, cardinality: 256},
		{rows: 65536, cardinality: 257},
		{rows: 65536, cardinality: 1024},
	}
	for _, shape := range shapes {
		benchmarkFloat32StatsShape(b, shape)
		benchmarkFloat64StatsShape(b, shape)
	}
}

func benchmarkFloat32StatsShape(b *testing.B, shape floatStatsBenchmarkShape) {
	b.Helper()
	arr := array.NewPrimitivesUnsafe(makeFloat32StatsValues(shape.rows, shape.cardinality))
	name := fmt.Sprintf("operation=kernel/type=float32/rows=%d/cardinality=%d", shape.rows, shape.cardinality)
	b.Run(name, func(b *testing.B) {
		benchmarkFloatStatsKernel(b, arr)
	})
	name = fmt.Sprintf("operation=compute-stats/type=float32/rows=%d/cardinality=%d", shape.rows, shape.cardinality)
	b.Run(name, func(b *testing.B) {
		benchmarkFloatCompressorStats(b, arr, float32Compressor())
	})
}

func benchmarkFloat64StatsShape(b *testing.B, shape floatStatsBenchmarkShape) {
	b.Helper()
	arr := array.NewPrimitivesUnsafe(makeFloat64StatsValues(shape.rows, shape.cardinality))
	name := fmt.Sprintf("operation=kernel/type=float64/rows=%d/cardinality=%d", shape.rows, shape.cardinality)
	b.Run(name, func(b *testing.B) {
		benchmarkFloatStatsKernel(b, arr)
	})
	name = fmt.Sprintf("operation=compute-stats/type=float64/rows=%d/cardinality=%d", shape.rows, shape.cardinality)
	b.Run(name, func(b *testing.B) {
		benchmarkFloatCompressorStats(b, arr, float64Compressor())
	})
}

func benchmarkFloatStatsKernel[T array.Float](b *testing.B, arr array.Array[T]) {
	b.Helper()
	var stats floatStats[T]
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		stats = computeFloatStatsForPlanner(arr, true)
	}
	b.StopTimer()
	if stats.distinctEstimate == 0 {
		b.Fatal("computeFloatStatsForPlanner returned no cardinality estimate")
	}
	runtime.KeepAlive(stats)
}

func benchmarkFloatCompressorStats[T array.Float](b *testing.B, arr array.Array[T], compressor floatCompressor[T]) {
	b.Helper()
	var stats floatStats[T]
	ctx := newPlanContext(Options{})
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		stats = compressor.ComputeStats(arr, ctx)
	}
	b.StopTimer()
	if stats.distinctEstimate == 0 {
		b.Fatal("floatCompressor.ComputeStats returned no cardinality estimate")
	}
	runtime.KeepAlive(stats)
}
