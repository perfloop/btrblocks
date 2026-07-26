package compress

import (
	"fmt"
	"strings"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/axiomhq/btrblocks/codec"
	"github.com/stretchr/testify/require"
)

func mustStrings(t testing.TB, values []string) array.Array[string] {
	t.Helper()
	arr, err := array.NewStrings(values)
	if err != nil {
		t.Fatalf("NewStrings: %v", err)
	}
	return arr
}

func TestCompressRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		arr  array.Array[uint32]
		want []uint32
	}{
		{name: "integers", arr: array.NewPrimitivesUnsafe([]uint32{1, 2, 1, 2}), want: []uint32{1, 2, 1, 2}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := UnsignedArray(tc.arr, Options{})
			require.NoError(t, err)
			decoded, err := codec.Decompress(encoded)
			require.NoError(t, err)
			require.Equal(t, tc.want, decoded)
		})
	}
}

func TestPlannerSelectsSparseForDominantInteger(t *testing.T) {
	values := make([]uint32, 1024)
	for i := range values {
		values[i] = 7
	}
	for i := 0; i < 100; i++ {
		values[i*10] = uint32(i + 1)
	}

	encoded, err := UnsignedArray(array.NewPrimitivesUnsafe(values), Options{})
	require.NoError(t, err)
	require.Equal(t, codec.CodecTypeSparse, encoded.CodecType())
	decoded, err := codec.Decompress(encoded)
	require.NoError(t, err)
	require.Equal(t, values, decoded)
}

func TestStringStatsFrequencyCapBoundary(t *testing.T) {
	// Three repetitions make the retained 4,096-key case prove that existing
	// keys continue accumulating counts without crossing the cap.
	const rows = 3 * maxRetainedStringDistinctValues
	cases := []struct {
		name              string
		distinct          int
		wantPlanner       codec.CodecType
		wantDistinctCount uint64
		wantMostFrequent  uint64
	}{
		{name: "retain-4096-and-count-existing-keys", distinct: maxRetainedStringDistinctValues, wantPlanner: codec.CodecTypeDict, wantDistinctCount: maxRetainedStringDistinctValues, wantMostFrequent: 3},
		{name: "overflow-at-4097-and-return-raw", distinct: maxRetainedStringDistinctValues + 1, wantPlanner: codec.CodecTypeRaw, wantDistinctCount: rows, wantMostFrequent: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			values := make([]string, rows)
			for i := range values {
				values[i] = fmt.Sprintf("stats-cap-%05d", i%tc.distinct)
			}

			stats := computeStringStatsForPlanner(mustStrings(t, values), true)
			require.Equal(t, tc.wantDistinctCount, stats.estimatedDistinctCount)
			require.Equal(t, tc.wantMostFrequent, stats.MostFrequentCount())

			encoded, err := StringArray(mustStrings(t, values), Options{})
			require.NoError(t, err)
			require.Equal(t, tc.wantPlanner, encoded.CodecType())
		})
	}
}

func TestPlannerBuildsOnlyWinner(t *testing.T) {
	values := make([]uint32, 512)
	for i := range values {
		values[i] = uint32(i % 4)
	}
	arr := array.NewPrimitivesUnsafe(values)
	var builds []codec.CodecType
	compressor := testCompressorUint32{
		schemes: []scheme[uint32, testStatsUint32]{
			{
				kind: codec.CodecTypeBitpack,
				estimate: func(testStatsUint32, planContext) schemeEstimate {
					return immediateEstimate(10, true)
				},
				build: func(arr array.ArrayCore[uint32], ctx planContext) (codec.EncodedArray[uint32], error) {
					builds = append(builds, codec.CodecTypeBitpack)
					return buildBitpack(arr, ctx)
				},
			},
			{
				kind: codec.CodecTypeFor,
				estimate: func(testStatsUint32, planContext) schemeEstimate {
					return immediateEstimate(2, true)
				},
				build: func(arr array.ArrayCore[uint32], ctx planContext) (codec.EncodedArray[uint32], error) {
					builds = append(builds, codec.CodecTypeFor)
					return buildFoR(arr, ctx)
				},
			},
		},
	}
	encoded, err := compressWith(arr, newPlanContext(Options{}), compressor)
	require.NoError(t, err)
	require.Equal(t, codec.CodecTypeBitpack, encoded.CodecType())
	require.Equal(t, []codec.CodecType{codec.CodecTypeBitpack}, builds)
}

func TestPlannerStopsAfterExactConstantDetection(t *testing.T) {
	arr := array.NewPrimitivesUnsafe([]uint32{7, 7, 7, 7})
	stats := testStatsUint32{arr: arr, isConstant: true}
	compressor := testCompressorUint32{
		schemes: []scheme[uint32, testStatsUint32]{
			{kind: codec.CodecTypeConst, estimate: func(testStatsUint32, planContext) schemeEstimate { return immediateEstimate(2, true) }},
			{kind: codec.CodecTypeBitpack, estimate: func(testStatsUint32, planContext) schemeEstimate {
				t.Fatal("evaluated a codec after exact constant detection")
				return skipEstimate()
			}},
		},
	}

	chosen, err := chooseScheme(arr, stats, newPlanContext(Options{}), compressor, nil)
	require.NoError(t, err)
	require.Equal(t, codec.CodecTypeConst, chosen.candidate.kind)
}

func TestSampleEstimateSkipsTinyArrays(t *testing.T) {
	stats := testStatsUint32{arr: array.NewPrimitivesUnsafe(make([]uint32, 16))}
	ctx := newPlanContext(Options{})
	estimate := estimateRunEnd(stats, ctx, 4, primitiveCanSample(stats.Source(), ctx))
	require.Equal(t, estimateSkip, estimate.kind)
}

func TestSampleEstimateDoesNotRecurse(t *testing.T) {
	stats := testStatsUint32{arr: array.NewPrimitivesUnsafe(make([]uint32, 64))}
	ctx := newPlanContext(Options{}).sampled()
	estimate := estimateRunEnd(stats, ctx, 4, primitiveCanSample(stats.Source(), ctx))
	require.Equal(t, estimateSkip, estimate.kind)
}

func TestFSSTEstimatorSkipsSubKilobyteInput(t *testing.T) {
	values := make([]string, 64)
	for i := range values {
		values[i] = fmt.Sprintf("value-%d", i)
	}
	stats := computeStringStatsForPlanner(mustStrings(t, values), true)
	estimate := estimateFSST(stats, newPlanContext(Options{}))
	require.Equal(t, estimateSkip, estimate.kind)
}

func TestFSSTEstimatorSkipsSmallValueCounts(t *testing.T) {
	values := make([]string, minFSSTInputValues-1)
	for i := range values {
		values[i] = strings.Repeat("compressible-value-", 16)
	}
	stats := computeStringStatsForPlanner(mustStrings(t, values), true)
	estimate := estimateFSST(stats, newPlanContext(Options{}))
	require.Equal(t, estimateSkip, estimate.kind)
}

func TestPlannerBitpacksTinyUnsignedArraysAnalytically(t *testing.T) {
	values := make([]uint8, 64)
	for i := range values {
		values[i] = uint8(i % 4)
	}
	encoded, err := UnsignedArray(array.NewPrimitivesUnsafe(values), Options{})
	require.NoError(t, err)
	require.Equal(t, codec.CodecTypeBitpack, encoded.CodecType())
	decoded, err := codec.Decompress(encoded)
	require.NoError(t, err)
	require.Equal(t, values, decoded)
}

type testStatsUint32 struct {
	arr        array.Array[uint32]
	isConstant bool
}

func (s testStatsUint32) Source() array.Array[uint32] { return s.arr }
func (s testStatsUint32) MostFrequentCount() uint64   { return 0 }
func (s testStatsUint32) IsConstant() bool            { return s.isConstant }

type testCompressorUint32 struct {
	schemes []scheme[uint32, testStatsUint32]
}

func (c testCompressorUint32) ComputeStats(arr array.Array[uint32], _ planContext) testStatsUint32 {
	return testStatsUint32{arr: arr}
}
func (testCompressorUint32) DefaultScheme() scheme[uint32, testStatsUint32] {
	return rawScheme[uint32, testStatsUint32]()
}
func (c testCompressorUint32) Schemes(testStatsUint32) schemeSet[uint32, testStatsUint32] {
	var set schemeSet[uint32, testStatsUint32]
	set.count = copy(set.values[:], c.schemes)
	return set
}
func (testCompressorUint32) IsExcluded(ctx planContext, kind codec.CodecType) bool {
	return ctx.excludesInteger(kind)
}

func (testCompressorUint32) RawEncodedSize(arr array.ArrayCore[uint32]) uint64 {
	return primitiveRawEncodedSize(arr)
}
