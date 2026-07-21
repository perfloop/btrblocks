package btrblocks

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/axiomhq/btrblocks/array"
)

type planContext struct {
	depth    int
	isSample bool
	// nullCount, when non-zero, marks the array being planned as a masked
	// nullable view: null rows hold synthetic zeros standing in for absent
	// values (compressFamilyNullable is the only setter). Its consumers are
	// the sequence estimator, which declines masked lanes (a sequence running
	// through synthetic zeros is coincidence, not structure),
	// shouldCollectFrequencies, which skips frequency collection when the
	// fill dominates, and the nullSparse builders, which size the dense lane
	// as length-nullCount. child clears it: codec children carry derived
	// payloads (codes, patches, validity bytes), not the masked view.
	nullCount uint64
	excludes  plannerExcludes
}

// Building several recursive candidates costs more than it can save on tiny
// arrays. Keep those arrays on exact analytical schemes and the raw fallback.
const minSampledPlanningBytes = 256

func newPlanContext(opts Options) planContext {
	opts = normalizeOptions(opts)
	return planContext{depth: opts.MaxDepth, excludes: opts.excludes}
}

func (c planContext) sampled() planContext { c.isSample = true; return c }

func (c planContext) withNullCount(nullCount uint64) planContext {
	c.nullCount = nullCount
	return c
}

func (c planContext) child(parent CodecType, childIndex uint8) planContext {
	if c.depth > 0 {
		c.depth--
	}
	excludes := childExclusions(parent, childIndex)
	c.excludes.integers |= excludes
	c.excludes.floats |= excludes
	c.excludes.strings |= excludes
	c.nullCount = 0
	return c
}

func (c planContext) excludesInteger(kind CodecType) bool { return c.excludes.integers.Has(kind) }
func (c planContext) excludesFloat(kind CodecType) bool   { return c.excludes.floats.Has(kind) }
func (c planContext) excludesString(kind CodecType) bool  { return c.excludes.strings.Has(kind) }

func primitiveArray[T array.PrimitiveType](arr array.ArrayCore[T]) (array.Array[T], error) {
	if full, ok := arr.(array.Array[T]); ok {
		return full, nil
	}
	return array.MaterializePrimitiveSlice(arr, 0, arr.Length())
}

func stringArray(arr array.ArrayCore[string]) (array.Array[string], error) {
	if full, ok := arr.(array.Array[string]); ok {
		return full, nil
	}
	return array.MaterializeStringSlice(arr, 0, arr.Length())
}

func newPrimitiveArray[T array.PrimitiveType](values []T) (array.Array[T], error) {
	return array.NewPrimitivesUnsafe(values), nil
}

// compressChildCore holds the shared child-compression body of the five
// families: the repeatedArrayCore→constArray fast path through constBody, then
// materialize through toFull and compress, each with a family-tagged error.
func compressChildCore[T array.Integer | array.Float | array.String](
	arr array.ArrayCore[T],
	ctx planContext,
	family string,
	constBody func([]T) (array.Array[T], error),
	toFull func(array.ArrayCore[T]) (array.Array[T], error),
	compressFull func(array.Array[T], planContext) (EncodedArray[T], error),
) (EncodedArray[T], error) {
	if repeated, ok := arr.(repeatedArrayCore[T]); ok && repeated.length != 0 {
		body, err := constBody([]T{repeated.value})
		if err != nil {
			return nil, fmt.Errorf("compress: build repeated %s child: %w", family, err)
		}
		return &constArray[T]{denseRows: denseRows(repeated.length), body: body}, nil
	}
	materialized, err := toFull(arr)
	if err != nil {
		return nil, fmt.Errorf("compress: materialize %s child: %w", family, err)
	}
	return compressFull(materialized, ctx)
}

func compressSignedCore[T array.SignedInteger](arr array.ArrayCore[T], ctx planContext) (EncodedArray[T], error) {
	return compressChildCore(arr, ctx, "signed", newPrimitiveArray[T], primitiveArray[T], compressSigned[T])
}

func compressUnsignedCore[T array.UnsignedInteger](arr array.ArrayCore[T], ctx planContext) (EncodedArray[T], error) {
	return compressChildCore(arr, ctx, "unsigned", newPrimitiveArray[T], primitiveArray[T], compressUnsigned[T])
}

func compressFloat32Core(arr array.ArrayCore[float32], ctx planContext) (EncodedArray[float32], error) {
	return compressChildCore(arr, ctx, "float32", newPrimitiveArray[float32], primitiveArray[float32], compressFloat32)
}

func compressFloat64Core(arr array.ArrayCore[float64], ctx planContext) (EncodedArray[float64], error) {
	return compressChildCore(arr, ctx, "float64", newPrimitiveArray[float64], primitiveArray[float64], compressFloat64)
}

func compressStringCore(arr array.ArrayCore[string], ctx planContext) (EncodedArray[string], error) {
	return compressChildCore(arr, ctx, "string", array.NewStrings, stringArray, compressString)
}

func rawEncoded[T array.Integer | array.Float | array.String](arr array.Array[T]) (EncodedArray[T], error) {
	return encodeRaw(arr), nil
}

// isExpectedBuildError reports whether err is a known "scheme doesn't apply"
// sentinel. These are safe to swallow with a raw fallback. Any other error
// indicates a bug in the codec and must propagate.
func isExpectedBuildError(err error) bool {
	return errors.Is(err, ErrDepthExhausted) ||
		errors.Is(err, ErrDataEmpty) ||
		errors.Is(err, ErrValueNotConstant) ||
		errors.Is(err, ErrNotArithmeticSequence) ||
		errors.Is(err, ErrALPHighPatchRatio) ||
		errors.Is(err, ErrALPRDHighPatchRatio)
}

// statsSource exposes the complete source array to the planner.
type statsSource[T array.Integer | array.Float | array.String] interface {
	Source() array.Array[T]
	IsConstant() bool
	MostFrequentCount() uint64
}

type estimateType uint8

const (
	estimateSkip estimateType = iota
	estimateAlways
	estimateImmediate
	estimateSample
	estimateDeferred
)

func (k estimateType) String() string {
	switch k {
	case estimateSkip:
		return "skip"
	case estimateAlways:
		return "always"
	case estimateImmediate:
		return "immediate"
	case estimateSample:
		return "sample"
	case estimateDeferred:
		return "deferred"
	default:
		return "unknown"
	}
}

// schemeEstimate separates cheap analytical verdicts from work that the
// selector must postpone until every immediate candidate has been ranked.
type schemeEstimate struct {
	ratio float64
	kind  estimateType
}

func skipEstimate() schemeEstimate { return schemeEstimate{} }

func alwaysEstimate() schemeEstimate { return schemeEstimate{kind: estimateAlways} }

func immediateEstimate(ratio float64, ok bool) schemeEstimate {
	if !ok {
		return skipEstimate()
	}
	return schemeEstimate{kind: estimateImmediate, ratio: ratio}
}

func sampleEstimate(ctx planContext) schemeEstimate {
	if ctx.isSample {
		return skipEstimate()
	}
	return schemeEstimate{kind: estimateSample}
}

func deferredEstimate(ctx planContext, ratio float64) schemeEstimate {
	if ctx.isSample {
		return skipEstimate()
	}
	return schemeEstimate{kind: estimateDeferred, ratio: ratio}
}

type schemeBuild[T array.Integer | array.Float | array.String] func(array.ArrayCore[T], planContext) (EncodedArray[T], error)

type schemeResolve[T array.Integer | array.Float | array.String, S statsSource[T]] func(array.Array[T], S, planContext, schemeEstimate, float64) (resolvedEstimate[T], error)

// scheme owns the complete lifecycle of an encoding candidate: its cheap
// estimate, optional deferred refinement, and final construction. Production
// scheme sets are initialized once per concrete primitive type so their
// generic function values do not escape once per column.
type scheme[T array.Integer | array.Float | array.String, S statsSource[T]] struct {
	kind     CodecType
	estimate func(stats S, ctx planContext) schemeEstimate
	resolve  schemeResolve[T, S]
	build    schemeBuild[T]
}

const maxSchemeCount = 8

// schemeSet keeps the small, fixed family candidate list on the stack. Some
// nested JSON workloads plan thousands of tiny arrays, where allocating one
// slice per array costs more than evaluating the candidates themselves.
type schemeSet[T array.Integer | array.Float | array.String, S statsSource[T]] struct {
	values [maxSchemeCount]scheme[T, S]
	count  int
}

// compressor owns stats generation, scheme registration, and exclusion policy
// for one type family.
type compressor[T array.Integer | array.Float | array.String, S statsSource[T]] interface {
	ComputeStats(arr array.Array[T], ctx planContext) S
	Schemes(stats S) schemeSet[T, S]
	IsExcluded(ctx planContext, kind CodecType) bool
	RawEncodedSize(array.ArrayCore[T]) uint64
}

// shouldCollectFrequencies reports whether c's ComputeStats should pay for
// value-frequency collection: skipped when nulls dominate the array or when
// both frequency consumers, Dict and Sparse, are excluded for c's family.
func shouldCollectFrequencies[T array.Integer | array.Float | array.String, S statsSource[T]](arr array.Array[T], ctx planContext, c compressor[T, S]) bool {
	return !countDominates(arr.Length(), ctx.nullCount) &&
		!(c.IsExcluded(ctx, CodecTypeDict) && c.IsExcluded(ctx, CodecTypeSparse))
}

func compressWith[T array.Integer | array.Float | array.String, S statsSource[T]](arr array.Array[T], ctx planContext, c compressor[T, S]) (EncodedArray[T], error) {
	return compressWithDiagnostics(arr, ctx, c, nil)
}

type resolvedEstimate[T array.Integer | array.Float | array.String] struct {
	encoded EncodedArray[T]
	ratio   float64
	ok      bool
}

type schemeSelection[T array.Integer | array.Float | array.String, S statsSource[T]] struct {
	candidate scheme[T, S]
	encoded   EncodedArray[T]
	ratio     float64
}

type selectorCandidateDiagnostic struct {
	kind     CodecType
	estimate estimateType
	ratio    float64
	selected bool
}

// selectorDiagnostics is deliberately package-private and fixed-size. Tests
// and benchmarks can explain selector decisions without adding allocations or
// production logging to the per-column hot path.
type selectorDiagnostics struct {
	candidates    [maxSchemeCount]selectorCandidateDiagnostic
	count         int
	selectedKind  CodecType
	resultKind    CodecType
	rawBytes      uint64
	resultBytes   uint64
	sampleCreated bool
}

func (d selectorDiagnostics) String() string {
	var result strings.Builder
	fmt.Fprintf(&result, "selected=%s result=%s raw=%d resultBytes=%d sample=%t", d.selectedKind, d.resultKind, d.rawBytes, d.resultBytes, d.sampleCreated)
	for _, candidate := range d.candidates[:d.count] {
		fmt.Fprintf(&result, " %s:%s:%.3f", candidate.kind, candidate.estimate, candidate.ratio)
		if candidate.selected {
			result.WriteByte('*')
		}
	}
	return result.String()
}

func compressWithDiagnostics[T array.Integer | array.Float | array.String, S statsSource[T]](arr array.Array[T], ctx planContext, c compressor[T, S], diagnostics *selectorDiagnostics) (EncodedArray[T], error) {
	stats := c.ComputeStats(arr, ctx)
	selection, err := chooseScheme(arr, stats, ctx, c, diagnostics)
	if err != nil {
		return nil, err
	}
	encoded := selection.encoded
	if encoded == nil {
		encoded, err = buildScheme(arr, ctx, selection.candidate)
		if err != nil {
			return nil, err
		}
	}
	rawBytes := c.RawEncodedSize(arr)
	result := rawFallback(arr, encoded, rawBytes)
	if diagnostics != nil {
		diagnostics.selectedKind = selection.candidate.kind
		diagnostics.resultKind = result.CodecType()
		diagnostics.rawBytes = rawBytes
		diagnostics.resultBytes = result.BinarySize()
	}
	return result, nil
}

// buildScheme builds s, falling back to a raw array on an expected "scheme
// doesn't apply" build error and propagating any other error.
func buildScheme[T array.Integer | array.Float | array.String, S statsSource[T]](arr array.Array[T], ctx planContext, s scheme[T, S]) (EncodedArray[T], error) {
	if s.kind == CodecTypeRaw {
		return rawEncoded(arr)
	}
	encoded, err := buildCandidate(arr, ctx, s)
	if err != nil {
		if !isExpectedBuildError(err) {
			return nil, err
		}
		return rawEncoded(arr)
	}
	return encoded, nil
}

// rawFallback returns a raw encoding of arr when encoded does not beat it.
func rawFallback[T array.Integer | array.Float | array.String](arr array.Array[T], encoded EncodedArray[T], rawBytes uint64) EncodedArray[T] {
	if encoded.BinarySize() >= rawBytes {
		raw, err := rawEncoded(arr)
		if err != nil {
			return nil
		}
		return raw
	}
	return encoded
}

func primitiveRawEncodedSize[T array.Integer | array.Float](arr array.ArrayCore[T]) uint64 {
	return uint64(headerSize) + primitiveRawBinarySize(arr)
}

func stringRawEncodedSize(arr array.ArrayCore[string]) uint64 {
	return uint64(headerSize) + stringRawBinarySize(arr)
}

func chooseScheme[T array.Integer | array.Float | array.String, S statsSource[T]](arr array.Array[T], stats S, ctx planContext, c compressor[T, S], diagnostics *selectorDiagnostics) (schemeSelection[T, S], error) {
	set := c.Schemes(stats)
	candidates := set.values[:set.count]
	if diagnostics != nil {
		*diagnostics = selectorDiagnostics{count: set.count}
		for i, candidate := range candidates {
			diagnostics.candidates[i].kind = candidate.kind
		}
	}
	// Constant detection scans the complete source and Const is the planner's
	// designated winner for that case. Short-circuit before sample-based
	// candidates recursively build codec trees. A constant sampled window is
	// not proof that the complete source is constant, hence the isSample guard.
	if !ctx.isSample && stats.IsConstant() && !c.IsExcluded(ctx, CodecTypeConst) {
		for i, candidate := range candidates {
			if candidate.kind == CodecTypeConst {
				if diagnostics != nil {
					diagnostics.candidates[i] = selectorCandidateDiagnostic{kind: candidate.kind, estimate: estimateAlways, selected: true}
				}
				return schemeSelection[T, S]{candidate: candidate, ratio: math.Inf(1)}, nil
			}
		}
	}

	best := schemeSelection[T, S]{candidate: rawScheme[T, S](), ratio: 1.0}
	bestRatio := 1.0
	var deferred [maxSchemeCount]int
	var estimates [maxSchemeCount]schemeEstimate
	deferredCount := 0
	for i, candidate := range candidates {
		if candidate.estimate == nil || c.IsExcluded(ctx, candidate.kind) {
			continue
		}
		estimate := candidate.estimate(stats, ctx)
		estimates[i] = estimate
		if diagnostics != nil {
			diagnostics.candidates[i].estimate = estimate.kind
			diagnostics.candidates[i].ratio = estimate.ratio
		}
		switch estimate.kind {
		case estimateAlways:
			best = schemeSelection[T, S]{candidate: candidate, ratio: math.Inf(1)}
			markSelectedDiagnostic(diagnostics, i)
			return best, nil
		case estimateImmediate:
			if validEstimateRatio(estimate.ratio) && estimate.ratio > bestRatio {
				best = schemeSelection[T, S]{candidate: candidate, ratio: estimate.ratio}
				bestRatio = estimate.ratio
			}
		case estimateSample, estimateDeferred:
			deferred[deferredCount] = i
			deferredCount++
		}
	}

	var sample array.ArrayCore[T]
	sampleCreated := false
	for _, index := range deferred[:deferredCount] {
		candidate := candidates[index]
		estimate := estimates[index]
		var resolved resolvedEstimate[T]
		var err error
		switch estimate.kind {
		case estimateSample:
			if !sampleCreated {
				sample = sampleArray(arr)
				sampleCreated = true
				if diagnostics != nil {
					diagnostics.sampleCreated = true
				}
			}
			resolved, err = estimateSampleCandidate(sample, candidate, ctx, c.RawEncodedSize)
		case estimateDeferred:
			if candidate.resolve == nil {
				return schemeSelection[T, S]{}, fmt.Errorf("compress: %s returned a deferred estimate without a resolver", candidate.kind)
			}
			resolved, err = candidate.resolve(arr, stats, ctx, estimate, bestRatio)
		}
		if err != nil {
			return schemeSelection[T, S]{}, fmt.Errorf("compress: estimating %s: %w", candidate.kind, err)
		}
		if diagnostics != nil && resolved.ok {
			diagnostics.candidates[index].ratio = resolved.ratio
		}
		if resolved.ok && validEstimateRatio(resolved.ratio) && resolved.ratio > bestRatio {
			best = schemeSelection[T, S]{candidate: candidate, encoded: resolved.encoded, ratio: resolved.ratio}
			bestRatio = resolved.ratio
		}
	}

	for i := range candidates {
		if candidates[i].kind == best.candidate.kind {
			markSelectedDiagnostic(diagnostics, i)
			break
		}
	}
	return best, nil
}

func markSelectedDiagnostic(diagnostics *selectorDiagnostics, index int) {
	if diagnostics != nil {
		diagnostics.candidates[index].selected = true
	}
}

func validEstimateRatio(ratio float64) bool {
	return ratio > 1.0 && !math.IsNaN(ratio) && !math.IsInf(ratio, 0)
}

func rawScheme[T array.Integer | array.Float | array.String, S statsSource[T]]() scheme[T, S] {
	return scheme[T, S]{kind: CodecTypeRaw}
}

func estimateSampleCandidate[T array.Integer | array.Float | array.String, S statsSource[T]](sample array.ArrayCore[T], candidate scheme[T, S], ctx planContext, rawSize func(array.ArrayCore[T]) uint64) (resolvedEstimate[T], error) {
	// A sampled candidate may recursively compress its children, but letting
	// those children start their own sampled candidate searches makes planning
	// grow combinatorially with codec depth. Analytical child estimates and the
	// raw fallback are sufficient for sizing the parent sample; the winning
	// top-level codec gets a full child search when it is built below.
	encoded, err := buildCandidate(sample, ctx.sampled(), candidate)
	if err != nil {
		if isExpectedBuildError(err) {
			return resolvedEstimate[T]{}, nil
		}
		return resolvedEstimate[T]{}, err
	}
	before := rawSize(sample)
	after := encoded.BinarySize()
	if after == 0 {
		return resolvedEstimate[T]{}, nil
	}
	return resolvedEstimate[T]{ratio: float64(before) / float64(after), ok: true}, nil
}

func buildCandidate[T array.Integer | array.Float | array.String, S statsSource[T]](arr array.ArrayCore[T], ctx planContext, candidate scheme[T, S]) (EncodedArray[T], error) {
	if candidate.build != nil {
		return candidate.build(arr, ctx)
	}
	return nil, fmt.Errorf("compress: %s has no build hook", candidate.kind)
}

func buildIntegerConst[T array.Integer](arr array.ArrayCore[T], _ planContext) (EncodedArray[T], error) {
	return encodeConstInteger(arr)
}

func buildFloatConst[T array.Float](arr array.ArrayCore[T], _ planContext) (EncodedArray[T], error) {
	return encodeConstFloat(arr)
}

func buildStringConst(arr array.ArrayCore[string], _ planContext) (EncodedArray[string], error) {
	return encodeConstString(arr)
}

func buildSequence[T array.Integer](arr array.ArrayCore[T], _ planContext) (EncodedArray[T], error) {
	return encodeSequence(arr)
}

func buildFoR[T array.Integer](arr array.ArrayCore[T], _ planContext) (EncodedArray[T], error) {
	return encodeFoR(arr)
}

type childCompressor[T array.Integer | array.Float | array.String] func(array.ArrayCore[T], planContext) (EncodedArray[T], error)

func buildDelta[T array.Integer](arr array.ArrayCore[T], ctx planContext, compressValues childCompressor[T]) (EncodedArray[T], error) {
	if ctx.depth <= 0 {
		return nil, ErrDepthExhausted
	}
	return encodeDelta(arr, func(child array.ArrayCore[T]) (EncodedArray[T], error) {
		return compressValues(child, ctx.child(CodecTypeDelta, 0))
	})
}

func buildIntegerDict[T array.Integer](arr array.ArrayCore[T], ctx planContext, compressValues childCompressor[T]) (EncodedArray[T], error) {
	if ctx.depth <= 0 {
		return nil, ErrDepthExhausted
	}
	return encodeIntegerDict(arr, nil, dictionaryChildren[T]{ctx: ctx, compressValues: compressValues})
}

func buildFloat32Dict(arr array.ArrayCore[float32], ctx planContext) (EncodedArray[float32], error) {
	if ctx.depth <= 0 {
		return nil, ErrDepthExhausted
	}
	return encodeFloat32Dict(arr, nil, 0, dictionaryChildren[float32]{ctx: ctx, compressValues: compressFloat32Core})
}

func buildFloat64Dict(arr array.ArrayCore[float64], ctx planContext) (EncodedArray[float64], error) {
	if ctx.depth <= 0 {
		return nil, ErrDepthExhausted
	}
	return encodeFloat64Dict(arr, nil, 0, dictionaryChildren[float64]{ctx: ctx, compressValues: compressFloat64Core})
}

func buildStringDict(arr array.ArrayCore[string], ctx planContext) (EncodedArray[string], error) {
	if ctx.depth <= 0 {
		return nil, ErrDepthExhausted
	}
	return encodeStringDict(arr, dictionaryChildren[string]{ctx: ctx, compressValues: compressStringCore})
}

type dictionaryChildren[T array.Integer | array.Float | array.String] struct {
	ctx            planContext
	compressValues childCompressor[T]
}

func (c dictionaryChildren[T]) BuildValues(child array.ArrayCore[T]) (EncodedArray[T], error) {
	return c.compressValues(child, c.ctx.child(CodecTypeDict, 0))
}

func (c dictionaryChildren[T]) BuildUint8(child array.ArrayCore[uint8]) (EncodedArray[uint8], error) {
	return compressUnsignedCore(child, c.ctx.child(CodecTypeDict, 1))
}

func (c dictionaryChildren[T]) BuildUint16(child array.ArrayCore[uint16]) (EncodedArray[uint16], error) {
	return compressUnsignedCore(child, c.ctx.child(CodecTypeDict, 1))
}

func (c dictionaryChildren[T]) BuildUint32(child array.ArrayCore[uint32]) (EncodedArray[uint32], error) {
	return compressUnsignedCore(child, c.ctx.child(CodecTypeDict, 1))
}

func buildPrimitiveRunEnd[T array.Integer | array.Float](arr array.ArrayCore[T], ctx planContext, cmp func(T, T) bool, compressValues childCompressor[T]) (EncodedArray[T], error) {
	if ctx.depth <= 0 {
		return nil, ErrDepthExhausted
	}
	if arr.Length() == 0 {
		return nil, ErrDataEmpty
	}
	return buildPrimitiveRunEndTyped(arr, ctx, cmp, compressValues)
}

func buildPrimitiveRunEndTyped[V array.Integer | array.Float](arr array.ArrayCore[V], ctx planContext, cmp func(V, V) bool, compressValues childCompressor[V]) (EncodedArray[V], error) {
	switch n := arr.Length(); {
	case n <= 1<<8:
		return buildPrimitiveRunEndAs[V, uint8](arr, ctx, cmp, compressValues)
	case n <= 1<<16:
		return buildPrimitiveRunEndAs[V, uint16](arr, ctx, cmp, compressValues)
	case n <= 1<<32:
		return buildPrimitiveRunEndAs[V, uint32](arr, ctx, cmp, compressValues)
	default:
		return buildPrimitiveRunEndAs[V, uint64](arr, ctx, cmp, compressValues)
	}
}

func buildPrimitiveRunEndAs[V array.Integer | array.Float, I array.UnsignedInteger](arr array.ArrayCore[V], ctx planContext, cmp func(V, V) bool, compressValues childCompressor[V]) (EncodedArray[V], error) {
	return encodePrimitiveRunEndAs[V, I](
		arr,
		cmp,
		func(child array.ArrayCore[V]) (EncodedArray[V], error) {
			return compressValues(child, ctx.child(CodecTypeRunEnd, 0))
		},
		func(child array.ArrayCore[I]) (EncodedArray[I], error) {
			return compressUnsignedCore(child, ctx.child(CodecTypeRunEnd, 1))
		},
	)
}

func buildStringRunEnd(arr array.ArrayCore[string], ctx planContext) (EncodedArray[string], error) {
	if ctx.depth <= 0 {
		return nil, ErrDepthExhausted
	}
	if arr.Length() == 0 {
		return nil, ErrDataEmpty
	}
	switch n := arr.Length(); {
	case n <= 1<<8:
		return buildStringRunEndAs[uint8](arr, ctx)
	case n <= 1<<16:
		return buildStringRunEndAs[uint16](arr, ctx)
	case n <= 1<<32:
		return buildStringRunEndAs[uint32](arr, ctx)
	default:
		return buildStringRunEndAs[uint64](arr, ctx)
	}
}

func buildStringRunEndAs[I array.UnsignedInteger](arr array.ArrayCore[string], ctx planContext) (EncodedArray[string], error) {
	return encodeStringRunEndAs[I](
		arr,
		array.CmpStrings[string],
		func(child array.ArrayCore[string]) (EncodedArray[string], error) {
			return compressStringCore(child, ctx.child(CodecTypeRunEnd, 0))
		},
		func(child array.ArrayCore[I]) (EncodedArray[I], error) {
			return compressUnsignedCore(child, ctx.child(CodecTypeRunEnd, 1))
		},
	)
}

func buildIntegerSparse[T array.Integer](arr array.ArrayCore[T], ctx planContext, compressValues childCompressor[T]) (EncodedArray[T], error) {
	if ctx.depth <= 0 {
		return nil, ErrDepthExhausted
	}
	return encodeIntegerSparse(arr, sparseChildren[T]{ctx: ctx, compressValues: compressValues})
}

func buildFloatSparse[T array.Float](arr array.ArrayCore[T], ctx planContext, compressValues childCompressor[T]) (EncodedArray[T], error) {
	if ctx.depth <= 0 {
		return nil, ErrDepthExhausted
	}
	return encodeFloatSparse(arr, sparseChildren[T]{ctx: ctx, compressValues: compressValues})
}

func buildStringSparse(arr array.ArrayCore[string], ctx planContext) (EncodedArray[string], error) {
	if ctx.depth <= 0 {
		return nil, ErrDepthExhausted
	}
	return encodeStringSparse(arr, sparseChildren[string]{ctx: ctx, compressValues: compressStringCore})
}

type sparseChildren[T array.Integer | array.Float | array.String] struct {
	ctx            planContext
	compressValues childCompressor[T]
}

func (c sparseChildren[T]) BuildFill(child array.ArrayCore[T]) (EncodedArray[T], error) {
	return c.compressValues(child, c.ctx.child(CodecTypeSparse, 0))
}

func (c sparseChildren[T]) BuildValues(child array.ArrayCore[T]) (EncodedArray[T], error) {
	return c.compressValues(child, c.ctx.child(CodecTypeSparse, 2))
}

func (c sparseChildren[T]) BuildUint8(child array.ArrayCore[uint8]) (EncodedArray[uint8], error) {
	return compressUnsignedCore(child, c.ctx.child(CodecTypeSparse, 1))
}

func (c sparseChildren[T]) BuildUint16(child array.ArrayCore[uint16]) (EncodedArray[uint16], error) {
	return compressUnsignedCore(child, c.ctx.child(CodecTypeSparse, 1))
}

func (c sparseChildren[T]) BuildUint32(child array.ArrayCore[uint32]) (EncodedArray[uint32], error) {
	return compressUnsignedCore(child, c.ctx.child(CodecTypeSparse, 1))
}

func (c sparseChildren[T]) BuildUint64(child array.ArrayCore[uint64]) (EncodedArray[uint64], error) {
	return compressUnsignedCore(child, c.ctx.child(CodecTypeSparse, 1))
}

func buildZigZag[T array.SignedInteger](arr array.ArrayCore[T], ctx planContext) (EncodedArray[T], error) {
	if ctx.depth <= 0 {
		return nil, ErrDepthExhausted
	}
	switch zigZagEncodedType(arr) {
	case PTypeUint8:
		return buildZigZagAs[T, uint8](arr, ctx)
	case PTypeUint16:
		return buildZigZagAs[T, uint16](arr, ctx)
	case PTypeUint32:
		return buildZigZagAs[T, uint32](arr, ctx)
	default:
		return buildZigZagAs[T, uint64](arr, ctx)
	}
}

func buildZigZagAs[T array.SignedInteger, U array.UnsignedInteger](arr array.ArrayCore[T], ctx planContext) (EncodedArray[T], error) {
	return encodeZigZagAs[T, U](arr, func(child array.ArrayCore[U]) (EncodedArray[U], error) {
		return compressUnsignedCore(child, ctx.child(CodecTypeZigZag, 0))
	})
}

func buildBitpack[T array.UnsignedInteger](arr array.ArrayCore[T], _ planContext) (EncodedArray[T], error) {
	return encodeBitpack(arr)
}

func buildALP32(arr array.ArrayCore[float32], ctx planContext) (EncodedArray[float32], error) {
	if ctx.depth <= 0 {
		return nil, ErrDepthExhausted
	}
	return encodeALP32(arr, alpChildren{ctx: ctx})
}

func buildALP64(arr array.ArrayCore[float64], ctx planContext) (EncodedArray[float64], error) {
	if ctx.depth <= 0 {
		return nil, ErrDepthExhausted
	}
	return encodeALP64(arr, alpChildren{ctx: ctx})
}

type alpChildren struct {
	ctx planContext
}

func (c alpChildren) encodedContext() planContext {
	child := c.ctx.child(CodecTypeALP, 0)
	if c.ctx.excludes.floats.Has(CodecTypeDict) {
		child.excludes.integers = child.excludes.integers.With(CodecTypeDict)
	}
	if c.ctx.excludes.floats.Has(CodecTypeRunEnd) {
		child.excludes.integers = child.excludes.integers.With(CodecTypeRunEnd)
	}
	return child
}

func (c alpChildren) BuildInt32(child array.ArrayCore[int32]) (EncodedArray[int32], error) {
	return compressSignedCore(child, c.encodedContext())
}

func (c alpChildren) BuildInt64(child array.ArrayCore[int64]) (EncodedArray[int64], error) {
	return compressSignedCore(child, c.encodedContext())
}

func (c alpChildren) BuildUint8(child array.ArrayCore[uint8]) (EncodedArray[uint8], error) {
	return compressUnsignedCore(child, c.ctx.child(CodecTypeALP, 1))
}

func (c alpChildren) BuildUint16(child array.ArrayCore[uint16]) (EncodedArray[uint16], error) {
	return compressUnsignedCore(child, c.ctx.child(CodecTypeALP, 1))
}

func (c alpChildren) BuildUint32(child array.ArrayCore[uint32]) (EncodedArray[uint32], error) {
	return compressUnsignedCore(child, c.ctx.child(CodecTypeALP, 1))
}

func (c alpChildren) BuildUint64(child array.ArrayCore[uint64]) (EncodedArray[uint64], error) {
	return compressUnsignedCore(child, c.ctx.child(CodecTypeALP, 1))
}

func buildALPRD32(arr array.ArrayCore[float32], ctx planContext) (EncodedArray[float32], error) {
	if ctx.depth <= 0 {
		return nil, ErrDepthExhausted
	}
	return encodeALPRD32(arr, alprdChildren{ctx: ctx})
}

func buildALPRD64(arr array.ArrayCore[float64], ctx planContext) (EncodedArray[float64], error) {
	if ctx.depth <= 0 {
		return nil, ErrDepthExhausted
	}
	return encodeALPRD64(arr, alprdChildren{ctx: ctx})
}

type alprdChildren struct {
	ctx planContext
}

func (c alprdChildren) BuildUint8(child array.ArrayCore[uint8]) (EncodedArray[uint8], error) {
	return compressUnsignedCore(child, c.ctx.child(CodecTypeALPRD, 0))
}

func (c alprdChildren) BuildUint16(child array.ArrayCore[uint16]) (EncodedArray[uint16], error) {
	return compressUnsignedCore(child, c.ctx.child(CodecTypeALPRD, 0))
}

func (c alprdChildren) BuildUint32(child array.ArrayCore[uint32]) (EncodedArray[uint32], error) {
	return compressUnsignedCore(child, c.ctx.child(CodecTypeALPRD, 0))
}

func (c alprdChildren) BuildUint64(child array.ArrayCore[uint64]) (EncodedArray[uint64], error) {
	return compressUnsignedCore(child, c.ctx.child(CodecTypeALPRD, 0))
}

func buildFSST(arr array.ArrayCore[string], ctx planContext) (EncodedArray[string], error) {
	if ctx.depth <= 0 {
		return nil, ErrDepthExhausted
	}
	return encodeFSST(
		arr,
		unsignedChildren{ctx: ctx, parent: CodecTypeFSST, childIndex: 0},
		unsignedChildren{ctx: ctx, parent: CodecTypeFSST, childIndex: 1},
	)
}

type unsignedChildren struct {
	ctx        planContext
	parent     CodecType
	childIndex uint8
}

func (c unsignedChildren) BuildUint8(child array.ArrayCore[uint8]) (EncodedArray[uint8], error) {
	return compressUnsignedCore(child, c.ctx.child(c.parent, c.childIndex))
}

func (c unsignedChildren) BuildUint16(child array.ArrayCore[uint16]) (EncodedArray[uint16], error) {
	return compressUnsignedCore(child, c.ctx.child(c.parent, c.childIndex))
}

func (c unsignedChildren) BuildUint32(child array.ArrayCore[uint32]) (EncodedArray[uint32], error) {
	return compressUnsignedCore(child, c.ctx.child(c.parent, c.childIndex))
}

func (c unsignedChildren) BuildUint64(child array.ArrayCore[uint64]) (EncodedArray[uint64], error) {
	return compressUnsignedCore(child, c.ctx.child(c.parent, c.childIndex))
}
