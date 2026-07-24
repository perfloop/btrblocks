package compress

import (
	"math"
	"math/bits"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

type floatStatsCase[T array.Float] struct {
	name   string
	values []T
}

type floatStatsExpectation struct {
	distinct         map[uint64]uint64
	distinctCount    uint64
	distinctEstimate uint64
	isConst          bool
	avgRunLength     float64
	mostFrequent     uint64
}

func TestComputeFloatStatsPreservesFrequencyAndSketchBoundaries(t *testing.T) {
	assertFloatStatsCases(t, []floatStatsCase[float64]{
		{name: "map-under-half-length-limit", values: makeFloat64StatsValues(64, 32)},
		{name: "map-over-half-length-limit", values: makeFloat64StatsValues(64, 33)},
		{name: "map-at-256-limit", values: makeFloat64StatsValues(65536, 256)},
		{name: "sketch-over-256-limit", values: makeFloat64StatsValues(65536, 257)},
		{name: "sketch-high-cardinality", values: makeFloat64StatsValues(65536, 1024)},
		{name: "nan-and-signed-zero", values: []float64{
			math.Float64frombits(0),
			math.Float64frombits(1 << 63),
			math.Float64frombits(0x7ff8000000000001),
			math.Float64frombits(0x7ff8000000000001),
			math.Float64frombits(0x7ff8000000000002),
			math.Float64frombits(0),
		}},
	})
	assertFloatStatsCases(t, []floatStatsCase[float32]{
		{name: "map-under-half-length-limit", values: makeFloat32StatsValues(64, 32)},
		{name: "map-over-half-length-limit", values: makeFloat32StatsValues(64, 33)},
		{name: "map-at-256-limit", values: makeFloat32StatsValues(65536, 256)},
		{name: "sketch-over-256-limit", values: makeFloat32StatsValues(65536, 257)},
		{name: "sketch-high-cardinality", values: makeFloat32StatsValues(65536, 1024)},
		{name: "nan-and-signed-zero", values: []float32{
			math.Float32frombits(0),
			math.Float32frombits(1 << 31),
			math.Float32frombits(0x7fc00001),
			math.Float32frombits(0x7fc00001),
			math.Float32frombits(0x7fc00002),
			math.Float32frombits(0),
		}},
	})
}

func assertFloatStatsCases[T array.Float](t *testing.T, cases []floatStatsCase[T]) {
	t.Helper()
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			arr := array.NewPrimitivesUnsafe(test.values)
			want := referenceFloatStats(test.values)

			got := computeFloatStatsForPlanner(arr, true)
			require.Equal(t, want.distinctCount, got.distinctCount)
			require.Equal(t, want.distinctEstimate, got.distinctEstimate)
			require.Equal(t, want.mostFrequent, got.mostFrequent)
			require.Equal(t, want.isConst, got.isConst)
			require.Equal(t, want.avgRunLength, got.avgRunLength)
			require.Equal(t, want.distinct, got.distinct)

			withoutFrequencies := computeFloatStatsForPlanner(arr, false)
			require.Equal(t, uint64(len(test.values)), withoutFrequencies.distinctCount)
			require.Equal(t, uint64(len(test.values)), withoutFrequencies.distinctEstimate)
			require.Zero(t, withoutFrequencies.mostFrequent)
			require.Equal(t, want.isConst, withoutFrequencies.isConst)
			require.Equal(t, want.avgRunLength, withoutFrequencies.avgRunLength)
			require.Nil(t, withoutFrequencies.distinct)
		})
	}
}

func referenceFloatStats[T array.Float](values []T) floatStatsExpectation {
	n := uint64(len(values))
	if n == 0 {
		return floatStatsExpectation{}
	}

	distinct := make(map[uint64]uint64, min(len(values), 256))
	var distinctBits [256]uint64
	runs := uint64(1)
	prev := values[0]
	distinct[array.FloatBits(prev)] = 1
	mostFrequent := uint64(1)

	for i, value := range values {
		key := array.FloatBits(value)
		hash := referenceMixFloatBits(key)
		bucket := hash & (floatDistinctSketchBuckets - 1)
		distinctBits[bucket/64] |= uint64(1) << (bucket & 63)
		if i == 0 {
			continue
		}
		if distinct != nil {
			if count, exists := distinct[key]; exists {
				count++
				distinct[key] = count
				mostFrequent = max(mostFrequent, count)
			} else if len(distinct) >= 256 || uint64(len(distinct)) >= n/2 {
				distinct = nil
			} else {
				distinct[key] = 1
			}
		}
		if !array.CmpFloatBits(value, prev) {
			runs++
			prev = value
		}
	}

	distinctCount := uint64(len(distinct))
	if distinct == nil {
		distinctCount = referenceFloatDistinct(distinctBits, n)
		mostFrequent = 0
	}
	return floatStatsExpectation{
		distinct:         distinct,
		distinctCount:    distinctCount,
		distinctEstimate: distinctCount,
		isConst:          runs == 1,
		avgRunLength:     float64(n) / float64(runs),
		mostFrequent:     mostFrequent,
	}
}

func referenceMixFloatBits(value uint64) uint64 {
	value += 0x9e3779b97f4a7c15
	value = (value ^ value>>30) * 0xbf58476d1ce4e5b9
	value = (value ^ value>>27) * 0x94d049bb133111eb
	return value ^ value>>31
}

func referenceFloatDistinct(bitmap [256]uint64, n uint64) uint64 {
	occupied := 0
	for _, word := range bitmap {
		occupied += bits.OnesCount64(word)
	}
	if occupied == floatDistinctSketchBuckets {
		return n
	}
	estimate := -float64(floatDistinctSketchBuckets) * math.Log(float64(floatDistinctSketchBuckets-occupied)/floatDistinctSketchBuckets)
	return min(n, uint64(estimate+0.5))
}

func makeFloat64StatsValues(n, cardinality int) []float64 {
	values := make([]float64, n)
	for i := range values {
		values[i] = math.Float64frombits(0x3ff0000000000000 + uint64((i*37)%cardinality))
	}
	return values
}

func makeFloat32StatsValues(n, cardinality int) []float32 {
	values := make([]float32, n)
	for i := range values {
		values[i] = math.Float32frombits(0x3f800000 + uint32((i*37)%cardinality))
	}
	return values
}
