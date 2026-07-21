package compress

import (
	"math/bits"

	"github.com/axiomhq/btrblocks/array"
)

func bitWidthForUnsigned(value uint64) uint { return uint(bits.Len64(value)) }

const (
	// FSST has a substantial fixed training cost and is intended for pages
	// large enough to amortize it.
	minFSSTInputBytes  = 1 << 10
	minFSSTInputValues = 1 << 10

	// Delta adds a dependent decode pass and a wrapper around its residuals.
	// Require both a useful input size and a material win over random-access
	// encodings before selecting it.
	minDeltaLength = 1024
	deltaPenalty   = 0.95
	minDeltaRatio  = 1.25
	// Delta trades random access for a dependent decode pass and recursively
	// plans its residual child. A marginal size estimate is not enough to pay
	// for that CPU and allocation cost.
	deltaSelectionMargin = 1.10
)

type integerFamily uint8

const (
	signedInteger integerFamily = iota
	unsignedInteger
)

func estimateConst[T array.Integer | array.Float | array.String](_ array.ArrayCore[T], ctx planContext, isConst bool) schemeEstimate {
	if ctx.isSample || !isConst {
		return skipEstimate()
	}
	return alwaysEstimate()
}

func estimateKnownSequence[T array.Integer](arr array.ArrayCore[T], isSequence bool, _, _ T) schemeEstimate {
	if !isSequence {
		return skipEstimate()
	}
	after := uint64(headerSize + 2*array.PTypeOfPrimitive[T]().ByteWidth())
	return immediateEstimate(float64(primitiveRawEncodedSize(arr))/float64(after), true)
}

func estimateFoR[T array.Integer](arr array.ArrayCore[T], ctx planContext, minValue, maxValue T, family integerFamily) schemeEstimate {
	if ctx.depth <= 0 {
		return skipEstimate()
	}
	fullWidth := uint(array.PTypeOfPrimitive[T]().ByteWidth()) * 8
	rangeWidth := bitWidthForUnsigned(uint64(maxValue) - uint64(minValue))
	if rangeWidth == 0 || rangeWidth >= fullWidth {
		return skipEstimate()
	}

	// Unsigned zero-based values gain nothing from a FoR wrapper. More
	// generally, direct Bitpack is preferable whenever subtracting the minimum
	// does not lower the packed width.
	if family == unsignedInteger {
		directWidth := bitWidthForUnsigned(uint64(maxValue))
		if minValue == 0 || rangeWidth >= directWidth {
			return skipEstimate()
		}
	}

	packedBytes := (arr.Length()*uint64(rangeWidth) + 7) / 8
	after := uint64(2*headerSize+1+array.PTypeOfPrimitive[T]().ByteWidth()) + packedBytes
	before := primitiveRawEncodedSize(arr)
	if after >= before {
		return skipEstimate()
	}
	return immediateEstimate(float64(before)/float64(after), true)
}

func estimateBitpack[T array.Integer](stats intStats[T], ctx planContext) schemeEstimate {
	if ctx.depth <= 0 {
		return skipEstimate()
	}
	bitWidth := bitWidthForUnsigned(uint64(stats.max))
	packedBytes := (stats.Source().Length()*uint64(bitWidth) + 7) / 8
	after := uint64(headerSize+1) + packedBytes
	before := primitiveRawEncodedSize(stats.Source())
	if after >= before {
		return skipEstimate()
	}
	return immediateEstimate(float64(before)/float64(after), true)
}

func estimateDelta[T array.Integer](stats intStats[T], ctx planContext, family integerFamily) schemeEstimate {
	n := stats.Source().Length()
	if ctx.depth <= 0 || n < minDeltaLength || stats.isSequence {
		return skipEstimate()
	}
	// The one-bit residual case is an upper bound. The deferred resolver uses
	// it to avoid scanning deltas when even the best possible result cannot
	// beat an immediate candidate.
	upperRatio := deltaRatioForWidth(stats.Source(), 1, 0, family)
	if upperRatio <= minDeltaRatio {
		return skipEstimate()
	}
	return deferredEstimate(ctx, upperRatio)
}

func resolveDeltaEstimate[T array.Integer](stats intStats[T], estimate schemeEstimate, bestRatio float64, family integerFamily) resolvedEstimate[T] {
	if estimate.ratio <= bestRatio*deltaSelectionMargin {
		return resolvedEstimate[T]{}
	}
	// Refinement is an optional second pass. Read through ArrayCore so virtual
	// arrays are not copied a second time solely to evaluate Delta.
	source := stats.Source()
	prev := source.ValueAt(0)
	value := source.ValueAt(1)
	deltaMin := value - prev
	deltaMax := deltaMin
	prev = value
	for i := range source.Length() - 2 {
		value = source.ValueAt(i + 2)
		delta := value - prev
		if delta < deltaMin {
			deltaMin = delta
		}
		if delta > deltaMax {
			deltaMax = delta
		}
		prev = value
	}
	rangeWidth := bitWidthForUnsigned(uint64(deltaMax) - uint64(deltaMin))
	if rangeWidth == 0 {
		return resolvedEstimate[T]{}
	}
	ratio := deltaRatioForWidth(stats.Source(), rangeWidth, deltaMin, family)
	if ratio <= minDeltaRatio || ratio <= bestRatio*deltaSelectionMargin {
		return resolvedEstimate[T]{}
	}
	return resolvedEstimate[T]{ratio: ratio, ok: true}
}

func deltaRatioForWidth[T array.Integer](source array.ArrayCore[T], rangeWidth uint, deltaMin T, family integerFamily) float64 {
	n := source.Length()
	elemBytes := uint64(array.PTypeOfPrimitive[T]().ByteWidth())
	packedBytes := ((n-1)*uint64(rangeWidth) + 7) / 8
	childBytes := uint64(headerSize+1) + packedBytes
	// Signed residuals need FoR before bitpacking. Unsigned residuals need it
	// whenever their minimum is non-zero.
	if family == signedInteger || deltaMin != 0 {
		childBytes += uint64(headerSize) + elemBytes
	}
	after := uint64(headerSize) + elemBytes + childBytes
	before := primitiveRawEncodedSize(source)
	if after >= before {
		return 0
	}
	return float64(before) / float64(after) * deltaPenalty
}

func estimateZigZag[T array.SignedInteger, S statsSource[T]](stats S, ctx planContext, hasNegative bool) schemeEstimate {
	if ctx.depth <= 0 || !hasNegative || !primitiveCanSample(stats.Source(), ctx) {
		return skipEstimate()
	}
	return sampleEstimate(ctx)
}

func estimateRunEnd[T array.Integer | array.Float | array.String, S statsSource[T]](_ S, ctx planContext, avgRunLength float64, sampleEligible bool) schemeEstimate {
	if ctx.depth <= 0 || avgRunLength < 4 || !sampleEligible {
		return skipEstimate()
	}
	return sampleEstimate(ctx)
}

func estimateSparseGeneric[T array.Integer | array.Float | array.String, S statsSource[T]](stats S, ctx planContext, sampleEligible bool) schemeEstimate {
	n := stats.Source().Length()
	if ctx.depth <= 0 || n == 0 || !sampleEligible || !countDominates(n, stats.MostFrequentCount()) {
		return skipEstimate()
	}
	return sampleEstimate(ctx)
}

// countDominates reports whether count covers at least 90% of length.
func countDominates(length, count uint64) bool {
	if length == 0 {
		return false
	}
	// length-length/10 is ceil(90% of length) without multiplication overflow.
	return min(count, length) >= length-length/10
}

func estimateIntegerDict[T array.Integer, S statsSource[T]](stats S, ctx planContext, distinctCount uint64, avgRunLength float64) schemeEstimate {
	if ctx.depth <= 0 {
		return skipEstimate()
	}
	n := stats.Source().Length()
	if n == 0 || distinctCount <= 1 || distinctCount > n/2 {
		return skipEstimate()
	}
	elemBitWidth := uint64(array.PTypeOfPrimitive[T]().ByteWidth()) * 8
	valuesCost := elemBitWidth * distinctCount
	codesBW := uint64(bitWidthForUnsigned(distinctCount - 1))
	codesCost := codesBW * n
	beforeBytes := primitiveRawEncodedSize(stats.Source())
	if beforeBytes < minSampledPlanningBytes {
		elemBytes := uint64(array.PTypeOfPrimitive[T]().ByteWidth())
		valuesBytes := min(
			uint64(headerSize)+2*elemBytes,
			uint64(headerSize+array.HeaderSize)+distinctCount*elemBytes,
		)
		indicesBytes := uint64(headerSize+1) + (codesBW*n+7)/8
		afterBytes := uint64(headerSize) + valuesBytes + indicesBytes
		if afterBytes >= beforeBytes {
			return skipEstimate()
		}
		return immediateEstimate(float64(beforeBytes)/float64(afterBytes), true)
	}
	if avgRunLength >= 4 {
		runCount := uint64(float64(n)/avgRunLength + 0.5)
		if runCount == 0 {
			runCount = 1
		}
		rleCost := (codesBW + 32) * runCount
		if rleCost < codesCost {
			codesCost = rleCost
		}
	}
	before := n * elemBitWidth
	after := valuesCost + codesCost
	if after >= before {
		return skipEstimate()
	}
	return immediateEstimate(float64(before)/float64(after), true)
}

func estimateIntegerDictBySample[T array.Integer, S statsSource[T]](stats S, ctx planContext) schemeEstimate {
	if ctx.depth <= 0 || !primitiveCanSample(stats.Source(), ctx) {
		return skipEstimate()
	}
	return sampleEstimate(ctx)
}

func estimateStringDict[S statsSource[string]](stats S, ctx planContext, estimatedDistinctCount uint64) schemeEstimate {
	n := stats.Source().Length()
	if ctx.depth <= 0 || n == 0 || estimatedDistinctCount > n/2 || !stringCanSample(stats.Source(), ctx) {
		return skipEstimate()
	}
	return sampleEstimate(ctx)
}

func estimateFloatDict[T array.Float](stats floatStats[T], ctx planContext) schemeEstimate {
	n := stats.Source().Length()
	distinctCount := stats.distinctEstimate
	if ctx.depth <= 0 || n == 0 || distinctCount <= 1 || distinctCount > 8192 || distinctCount > n/2 {
		return skipEstimate()
	}
	elemBitWidth := uint64(array.PTypeOfPrimitive[T]().ByteWidth()) * 8
	after := elemBitWidth*distinctCount + uint64(bitWidthForUnsigned(distinctCount-1))*n
	before := elemBitWidth * n
	if after >= before {
		return skipEstimate()
	}
	// Full-column cardinality gives a much better dictionary signal than a
	// small sample. Defer the exact build so it can compare against ALP/ALPRD's
	// sampled scores and be skipped when it is not competitive.
	return deferredEstimate(ctx, float64(before)/float64(after))
}

func estimateALP[T array.Float, S statsSource[T]](stats S, ctx planContext, isConst bool) schemeEstimate {
	if ctx.depth <= 0 || isConst || !primitiveCanSample(stats.Source(), ctx) {
		return skipEstimate()
	}
	return sampleEstimate(ctx)
}

func estimateALPRD[T array.Float, S statsSource[T]](stats S, ctx planContext, isConst bool) schemeEstimate {
	if ctx.depth <= 0 || isConst || !primitiveCanSample(stats.Source(), ctx) {
		return skipEstimate()
	}
	return sampleEstimate(ctx)
}

func estimateFSST(stats stringStats, ctx planContext) schemeEstimate {
	if ctx.depth <= 0 || stats.Source().Length() < minFSSTInputValues || stats.totalBytes < minFSSTInputBytes || !stringCanSample(stats.Source(), ctx) {
		return skipEstimate()
	}
	return sampleEstimate(ctx)
}

func primitiveCanSample[T array.Integer | array.Float](source array.ArrayCore[T], ctx planContext) bool {
	return !ctx.isSample && primitiveRawEncodedSize(source) >= minSampledPlanningBytes
}

func stringCanSample(source array.ArrayCore[string], ctx planContext) bool {
	return !ctx.isSample && stringRawEncodedSize(source) >= minSampledPlanningBytes
}
