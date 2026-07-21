package compress

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"runtime"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/axiomhq/btrblocks/codec"
	"github.com/stretchr/testify/require"
)

func TestPlannerPropagatesBuildBudget(t *testing.T) {
	source := array.NewVirtual(2, func(i uint64) uint64 { return i + 1 })
	ctx := newPlanContext(Options{}.WithMaxBuildBytes(8))
	if _, err := buildFoR(source, ctx); !errors.Is(err, ErrMaterializationLimit) {
		t.Fatalf("buildFoR error = %v, want %v", err, ErrMaterializationLimit)
	}
	if _, err := buildFoR(source, newPlanContext(Options{}.WithMaxBuildBytes(16))); err != nil {
		t.Fatalf("buildFoR with sufficient budget: %v", err)
	}
}

func TestPlannerDefersSamplingUntilAfterImmediateEstimates(t *testing.T) {
	values := make([]uint32, 4096)
	for i := range values {
		values[i] = uint32(i % 16)
	}
	events := make([]string, 0, 1)
	arr := array.NewPrimitivesUnsafe(values)
	stats := testStatsUint32{arr: arr}
	compressor := testCompressorUint32{schemes: []scheme[uint32, testStatsUint32]{
		{kind: CodecTypeBitpack, build: buildBitpack[uint32], estimate: func(_ testStatsUint32, ctx planContext) schemeEstimate { return sampleEstimate(ctx) }},
		{kind: CodecTypeDict, build: func(arr array.ArrayCore[uint32], ctx planContext) (EncodedArray[uint32], error) {
			return buildIntegerDict(arr, ctx, compressUnsignedCore[uint32])
		}, estimate: func(_ testStatsUint32, ctx planContext) schemeEstimate { return sampleEstimate(ctx) }},
		{kind: CodecTypeFor, estimate: func(testStatsUint32, planContext) schemeEstimate {
			events = append(events, "immediate")
			return immediateEstimate(1.01, true)
		}},
	}}
	var diagnostics selectorDiagnostics
	_, err := chooseScheme(arr, stats, newPlanContext(Options{}), compressor, &diagnostics)
	require.NoError(t, err)
	require.NotEmpty(t, events)
	require.Equal(t, "immediate", events[0], diagnostics.String())
	require.True(t, diagnostics.sampleCreated)
}

func TestSelectorDoesNotCreateSampleForImmediateWinner(t *testing.T) {
	values := make([]uint32, 4096)
	for i := range values {
		values[i] = uint32(i % 16)
	}
	arr := array.NewPrimitivesUnsafe(values)
	stats := testStatsUint32{arr: arr}
	compressor := testCompressorUint32{schemes: []scheme[uint32, testStatsUint32]{
		{kind: CodecTypeBitpack, estimate: func(testStatsUint32, planContext) schemeEstimate {
			return immediateEstimate(4, true)
		}},
	}}
	var diagnostics selectorDiagnostics
	selection, err := chooseScheme(arr, stats, newPlanContext(Options{}), compressor, &diagnostics)
	require.NoError(t, err)
	require.Equal(t, CodecTypeBitpack, selection.candidate.kind)
	require.False(t, diagnostics.sampleCreated)
}

func TestSelectorDefersExactFloatDictionary(t *testing.T) {
	distinct := [...]float64{1.1, 20.2, 300.3, 4000.4, 50000.5}
	values := make([]float64, 4096)
	for i := range values {
		values[i] = distinct[i%len(distinct)]
	}
	arr := array.NewPrimitivesUnsafe(values)
	var diagnostics selectorDiagnostics
	encoded, err := compressWithDiagnostics(arr, newPlanContext(Options{}), float64Compressor(), &diagnostics)
	require.NoError(t, err)
	require.Equal(t, CodecTypeDict, encoded.CodecType(), diagnostics.String())
	require.False(t, diagnostics.sampleCreated, diagnostics.String())

	dictDiagnostic, ok := findCandidateDiagnostic(diagnostics, CodecTypeDict)
	require.True(t, ok)
	require.Equal(t, estimateDeferred, dictDiagnostic.estimate)
	require.True(t, dictDiagnostic.selected)
}

func TestDeferredFloatDictionaryIsDeterministic(t *testing.T) {
	values := make([]float64, 4096)
	for i := range values {
		values[i] = float64((i * 37) % 64)
	}
	arr := array.NewPrimitivesUnsafe(values)
	var buffers [2]bytes.Buffer
	for i := range buffers {
		encoded, err := Float64Array(arr, Options{})
		require.NoError(t, err)
		require.Equal(t, CodecTypeDict, encoded.CodecType())
		_, err = encoded.WriteTo(&buffers[i])
		require.NoError(t, err)
	}
	require.Equal(t, buffers[0].Bytes(), buffers[1].Bytes())
}

func TestFloatDictionaryDeferredEstimateHonorsIncumbent(t *testing.T) {
	values := make([]float64, 4096)
	for i := range values {
		values[i] = float64(i % 64)
	}
	arr := array.NewPrimitivesUnsafe(values)
	stats := computeFloatStatsForPlanner(arr, true)
	estimate := estimateFloatDict(stats, newPlanContext(Options{}))
	require.Equal(t, estimateDeferred, estimate.kind)

	resolved, err := resolveFloat64Dictionary(
		arr,
		stats,
		newPlanContext(Options{}),
		estimate,
		estimate.ratio/floatDictionaryRefineMargin+1,
	)
	require.NoError(t, err)
	require.False(t, resolved.ok)
	require.Nil(t, resolved.encoded)
}

func TestSelectorFoRRedundancyAndDeltaCostPolicy(t *testing.T) {
	t.Run("zero-based unsigned uses bitpack", func(t *testing.T) {
		values := make([]uint32, 4096)
		for i := range values {
			values[i] = uint32(i % 16)
		}
		encoded, diagnostics := compressWithRootDiagnostics(t, array.NewPrimitivesUnsafe(values), &unsignedIntCompressor[uint32]{})
		require.Equal(t, CodecTypeBitpack, encoded.CodecType(), diagnostics.String())
		forDiagnostic, ok := findCandidateDiagnostic(diagnostics, CodecTypeFor)
		require.True(t, ok)
		require.Equal(t, estimateSkip, forDiagnostic.estimate)
	})

	t.Run("short irregular sequence skips delta", func(t *testing.T) {
		values := make([]uint64, minDeltaLength-1)
		var value uint64 = 1 << 40
		for i := range values {
			value += uint64(i%7 + 1)
			values[i] = value
		}
		_, diagnostics := compressWithRootDiagnostics(t, array.NewPrimitivesUnsafe(values), &unsignedIntCompressor[uint64]{})
		deltaDiagnostic, ok := findCandidateDiagnostic(diagnostics, CodecTypeDelta)
		require.True(t, ok)
		require.Equal(t, estimateSkip, deltaDiagnostic.estimate)
	})

	t.Run("long smooth integers use delta", func(t *testing.T) {
		values := make([]uint64, 4096)
		var value uint64 = 1 << 40
		for i := range values {
			value += uint64(i%7 + 1)
			values[i] = value
		}
		encoded, diagnostics := compressWithRootDiagnostics(t, array.NewPrimitivesUnsafe(values), &unsignedIntCompressor[uint64]{})
		require.Equal(t, CodecTypeDelta, encoded.CodecType(), diagnostics.String())
		deltaDiagnostic, ok := findCandidateDiagnostic(diagnostics, CodecTypeDelta)
		require.True(t, ok)
		require.Equal(t, estimateDeferred, deltaDiagnostic.estimate)
	})
}

func TestSelectorGoldenEncodings(t *testing.T) {
	t.Run("constant", func(t *testing.T) {
		assertSelectorGolden(t, array.NewPrimitivesUnsafe([]uint32{42, 42, 42, 42}), CodecTypeConst, &unsignedIntCompressor[uint32]{})
	})

	t.Run("sequence", func(t *testing.T) {
		values := make([]int64, 4096)
		for i := range values {
			values[i] = -1_000_000 + int64(i)*7
		}
		assertSelectorGolden(t, array.NewPrimitivesUnsafe(values), CodecTypeSequence, &signedIntCompressor[int64]{})
	})

	t.Run("for", func(t *testing.T) {
		values := make([]uint64, 4096)
		for i := range values {
			values[i] = 1<<40 + uint64((i*37)%101)
		}
		assertSelectorGolden(t, array.NewPrimitivesUnsafe(values), CodecTypeFor, &unsignedIntCompressor[uint64]{})
	})

	t.Run("bitpack", func(t *testing.T) {
		values := make([]uint32, 4096)
		for i := range values {
			values[i] = uint32(i % 16)
		}
		assertSelectorGolden(t, array.NewPrimitivesUnsafe(values), CodecTypeBitpack, &unsignedIntCompressor[uint32]{})
	})

	t.Run("sparse", func(t *testing.T) {
		values := make([]uint32, 4096)
		for i := range values {
			values[i] = 7
		}
		for i := 0; i < 200; i++ {
			values[i*20] = uint32(i + 1000)
		}
		assertSelectorGolden(t, array.NewPrimitivesUnsafe(values), CodecTypeSparse, &unsignedIntCompressor[uint32]{})
	})

	t.Run("dictionary", func(t *testing.T) {
		choices := [...]int32{17, 1_000_003, -700_001, 81_234_567, -91_111_113, 2_000_000_011}
		values := make([]int32, 4096)
		for i := range values {
			values[i] = choices[(i*37+i/11)%len(choices)]
		}
		assertSelectorGolden(t, array.NewPrimitivesUnsafe(values), CodecTypeDict, &signedIntCompressor[int32]{})
	})

	t.Run("delta", func(t *testing.T) {
		values := make([]uint64, 4096)
		var value uint64 = 1 << 40
		for i := range values {
			value += uint64(i%7 + 1)
			values[i] = value
		}
		assertSelectorGolden(t, array.NewPrimitivesUnsafe(values), CodecTypeDelta, &unsignedIntCompressor[uint64]{})
	})

	t.Run("alp", func(t *testing.T) {
		values := make([]float64, 4096)
		for i := range values {
			values[i] = float64(i) * 0.01
		}
		assertSelectorGolden(t, array.NewPrimitivesUnsafe(values), CodecTypeALP, float64Compressor())
	})

	t.Run("alprd", func(t *testing.T) {
		values := makeALPRDSelectorValues(4096)
		assertSelectorGolden(t, array.NewPrimitivesUnsafe(values), CodecTypeALPRD, float64Compressor())
	})

	t.Run("fsst", func(t *testing.T) {
		values := make([]string, 4096)
		for i := range values {
			values[i] = fmt.Sprintf("https://logs.example.com/v1/tenant/common/path/event/%08d", i)
		}
		assertSelectorGolden(t, mustStrings(t, values), CodecTypeFSST, stringCompressor{})
	})
}

func BenchmarkSelector(b *testing.B) {
	b.Run("type=uint64/distribution=delta/rows=65536", func(b *testing.B) {
		values := make([]uint64, 65536)
		var value uint64 = 1 << 40
		for i := range values {
			value += uint64(i%7 + 1)
			values[i] = value
		}
		benchmarkSelectorArray(b, array.NewPrimitivesUnsafe(values), &unsignedIntCompressor[uint64]{})
	})
	b.Run("type=float64/distribution=low-cardinality/rows=65536", func(b *testing.B) {
		values := make([]float64, 65536)
		for i := range values {
			values[i] = float64((i * 37) % 512)
		}
		benchmarkSelectorArray(b, array.NewPrimitivesUnsafe(values), float64Compressor())
	})
	b.Run("type=float64/distribution=repeated-mantissa/rows=65536", func(b *testing.B) {
		values := makeALPRDSelectorValues(65536)
		benchmarkSelectorArray(b, array.NewPrimitivesUnsafe(values), float64Compressor())
	})
	b.Run("type=string/distribution=common-prefix/rows=65536", func(b *testing.B) {
		values := make([]string, 65536)
		for i := range values {
			values[i] = fmt.Sprintf("https://logs.example.com/v1/tenant/common/path/event/%08d", i)
		}
		benchmarkSelectorArray(b, mustStrings(b, values), stringCompressor{})
	})
}

func makeALPRDSelectorValues(n int) []float64 {
	values := make([]float64, n)
	state := uint64(1)
	for i := range values {
		state = state*6364136223846793005 + 1442695040888963407
		prefix := uint64(0x3ff0 + i%2)
		values[i] = math.Float64frombits(prefix<<48 | state&((1<<48)-1))
	}
	return values
}

func benchmarkSelectorArray[T array.Integer | array.Float | array.String, S statsSource[T]](b *testing.B, arr array.Array[T], c compressor[T, S]) {
	b.Helper()
	var encoded EncodedArray[T]
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		encoded, _ = compressWithRootDiagnostics(b, arr, c)
	}
	b.StopTimer()
	b.ReportMetric(float64(encoded.BinarySize()), "encoded-bytes/op")
	runtime.KeepAlive(encoded)
}

func assertSelectorGolden[T array.Integer | array.Float | array.String, S statsSource[T]](t *testing.T, arr array.Array[T], want CodecType, c compressor[T, S]) {
	t.Helper()
	encoded, diagnostics := compressWithRootDiagnostics(t, arr, c)
	require.Equal(t, want, encoded.CodecType(), diagnostics.String())
	decoded, err := codec.Decompress(encoded)
	require.NoError(t, err)
	wantValues := make([]T, arr.Length())
	arr.CopyTo(wantValues)
	require.Equal(t, wantValues, decoded)
}

func compressWithRootDiagnostics[T array.Integer | array.Float | array.String, S statsSource[T]](t testing.TB, arr array.Array[T], c compressor[T, S]) (EncodedArray[T], selectorDiagnostics) {
	t.Helper()
	ctx := newPlanContext(Options{})
	var diagnostics selectorDiagnostics
	result, err := compressWithDiagnostics(arr, ctx, c, &diagnostics)
	if err != nil {
		t.Fatalf("compress: %v (%s)", err, diagnostics.String())
	}
	return result, diagnostics
}

func findCandidateDiagnostic(diagnostics selectorDiagnostics, kind CodecType) (selectorCandidateDiagnostic, bool) {
	for _, candidate := range diagnostics.candidates[:diagnostics.count] {
		if candidate.kind == kind {
			return candidate, true
		}
	}
	return selectorCandidateDiagnostic{}, false
}
