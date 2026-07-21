package compress

import "github.com/axiomhq/btrblocks/array"

const (
	// sampleWindow is the number of contiguous elements per sampling window.
	// Large enough to capture local patterns (runs, small dicts) while
	// keeping total sample overhead well below 1% of the source array.
	sampleWindow = 64
	// minSampleRuns is the minimum number of windows to draw, guaranteeing
	// at least minSampleRuns * sampleWindow = 1024 elements are examined.
	// Provides enough coverage for stable scheme estimation on small arrays.
	minSampleRuns = 16

	// LCG parameters from Knuth's MMIX (used by numpy, glibc). Fixed seed
	// ensures deterministic, reproducible sampling across runs.
	sampleSeed   = 1234567890
	sampleLCGMul = 6364136223846793005
	sampleLCGInc = 1442695040888963407
)

// indexRange represents a half-open interval within the source array.
type indexRange struct {
	start uint64
	end   uint64
}

// sampledArray is a small ranged view used for stratified planner samples.
// It implements only array.ArrayCore[T] — it is not serializable or sliceable.
type sampledArray[T array.Integer | array.Float | array.String] struct {
	source  array.Array[T]
	length  uint64
	offsets []uint64
	ranges  []indexRange
}

func newSampledArray[T array.Integer | array.Float | array.String](source array.Array[T], ranges []indexRange) array.ArrayCore[T] {
	if len(ranges) == 1 && ranges[0].start == 0 && ranges[0].end == source.Length() {
		return source
	}
	offsets := make([]uint64, len(ranges)+1)
	var length uint64
	for i, selected := range ranges {
		offsets[i] = length
		length += selected.end - selected.start
	}
	offsets[len(ranges)] = length
	return &sampledArray[T]{
		source:  source,
		length:  length,
		offsets: offsets,
		ranges:  ranges,
	}
}

func (a *sampledArray[T]) ValueAt(offset uint64) T {
	if offset >= a.length {
		panic(errOffsetOutOfRange)
	}
	return a.source.ValueAt(a.sourceOffset(offset))
}

func (a *sampledArray[T]) sourceOffset(offset uint64) uint64 {
	// Binary search on sorted offsets to find the chunk containing offset.
	// offsets has len(chunks)+1 entries; we want the largest i where offsets[i] <= offset.
	lo, hi := 0, len(a.ranges)
	for lo < hi {
		mid := lo + (hi-lo)/2
		if a.offsets[mid+1] <= offset {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return a.ranges[lo].start + offset - a.offsets[lo]
}

func (a *sampledArray[T]) IsValid(offset uint64) bool {
	if offset >= a.length {
		panic(errOffsetOutOfRange)
	}
	return a.source.IsValid(a.sourceOffset(offset))
}

func (a *sampledArray[T]) NullCount() uint64 {
	var count uint64
	for i := range a.length {
		if !a.IsValid(i) {
			count++
		}
	}
	return count
}

func (a *sampledArray[T]) Length() uint64 { return a.length }

func primitiveRawBinarySize[T array.Integer | array.Float](arr array.ArrayCore[T]) uint64 {
	return array.HeaderSize + arr.Length()*uint64(array.PTypeOfPrimitive[T]().ByteWidth())
}

func stringRawBinarySize(arr array.ArrayCore[string]) uint64 {
	if full, ok := arr.(array.Array[string]); ok {
		return full.BinarySize()
	}
	return computeStringRawBinarySize(arr)
}

func computeStringRawBinarySize(arr array.ArrayCore[string]) uint64 {
	n := arr.Length()
	totalBytes := uint64(0)
	for i := range n {
		totalBytes += uint64(len(arr.ValueAt(i)))
	}
	offsetWidth := uint64(4)
	switch {
	case totalBytes <= uint64(^uint8(0)):
		offsetWidth = 1
	case totalBytes <= uint64(^uint16(0)):
		offsetWidth = 2
	}
	return array.HeaderSize + 4 + (n+1)*offsetWidth + totalBytes
}

func sampleCountApproxOnePercent(length uint64) uint64 {
	if length == 0 {
		return minSampleRuns
	}
	approx := (length / 100) / sampleWindow
	if approx == 0 {
		approx = minSampleRuns
	}
	// Align to multiples of minSampleRuns for consistent stride patterns.
	if approx%minSampleRuns != 0 {
		approx += minSampleRuns - (approx % minSampleRuns)
	}
	if approx < minSampleRuns {
		return minSampleRuns
	}
	return approx
}

func partitionIndices(length, partitions uint64) []indexRange {
	if partitions == 0 {
		return nil
	}
	longParts := length % partitions
	shortStep := length / partitions
	longStep := shortStep + 1
	longStop := longParts * longStep

	ranges := make([]indexRange, 0, partitions)
	for off := uint64(0); off < longStop; off += longStep {
		ranges = append(ranges, indexRange{start: off, end: off + longStep})
	}
	if shortStep == 0 {
		return ranges
	}
	for off := longStop; off < length; off += shortStep {
		ranges = append(ranges, indexRange{start: off, end: off + shortStep})
	}
	return ranges
}

func sampleArray[T array.Integer | array.Float | array.String](arr array.Array[T]) array.ArrayCore[T] {
	length := arr.Length()
	sampleRuns := sampleCountApproxOnePercent(length)
	totalSample := uint64(sampleWindow) * sampleRuns
	if totalSample >= length {
		return arr
	}

	partitions := partitionIndices(length, sampleRuns)
	selected := make([]indexRange, 0, len(partitions))
	rng := uint64(sampleSeed)

	for _, part := range partitions {
		partLen := part.end - part.start
		if partLen == 0 {
			continue
		}
		take := min(partLen, uint64(sampleWindow))
		rng = rng*sampleLCGMul + sampleLCGInc
		offset := uint64(0)
		if partLen > take {
			offset = (rng >> 33) % (partLen - take + 1)
		}
		start := part.start + offset
		selected = append(selected, indexRange{start: start, end: start + take})
	}
	if len(selected) == 0 {
		return arr
	}
	return newSampledArray(arr, selected)
}
