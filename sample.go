package btrblocks

import "github.com/axiomhq/btrblocks/array"

const (
	sampleWindow  = 64
	minSampleRuns = 16

	sampleSeed   = 1234567890
	sampleLCGMul = 6364136223846793005
	sampleLCGInc = 1442695040888963407
)

// indexRange represents a half-open interval within the source array.
type indexRange struct {
	start uint64
	end   uint64
}

// sampledArray is a small chunked array used for stratified planner samples.
// It implements only array.ArrayCore[T] — it is not serializable or sliceable.
type sampledArray[T Integer | Float | String] struct {
	pType   PType
	length  uint64
	offsets []uint64
	chunks  []array.Array[T]
}

func newSampledArray[T Integer | Float | String](chunks []array.Array[T]) array.ArrayCore[T] {
	if len(chunks) == 1 {
		return chunks[0]
	}
	offsets := make([]uint64, len(chunks)+1)
	var length uint64
	for i, chunk := range chunks {
		offsets[i] = length
		length += chunk.Length()
	}
	offsets[len(chunks)] = length
	return &sampledArray[T]{
		pType:   chunks[0].PType(),
		length:  length,
		offsets: offsets,
		chunks:  chunks,
	}
}

func (a *sampledArray[T]) ValueAt(offset uint64) T {
	if offset >= a.length {
		panic(errOffsetOutOfRange)
	}
	// Binary search on sorted offsets to find the chunk containing offset.
	// offsets has len(chunks)+1 entries; we want the largest i where offsets[i] <= offset.
	lo, hi := 0, len(a.chunks)
	for lo < hi {
		mid := lo + (hi-lo)/2
		if a.offsets[mid+1] <= offset {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return a.chunks[lo].ValueAt(offset - a.offsets[lo])
}

func (a *sampledArray[T]) Length() uint64 { return a.length }

// rawBinarySize estimates the uncompressed serialized size of an ArrayCore.
// For primitives: header + length * element width.
// For strings: computed by scanning string lengths.
func rawBinarySize[T Integer | Float | String](arr array.ArrayCore[T]) uint64 {
	var zero T
	switch any(zero).(type) {
	case string:
		totalBytes := uint64(0)
		for i := uint64(0); i < arr.Length(); i++ {
			totalBytes += uint64(len(any(arr.ValueAt(i)).(string)))
		}
		offsetWidth := uint64(4)
		switch {
		case totalBytes <= uint64(^uint8(0)):
			offsetWidth = 1
		case totalBytes <= uint64(^uint16(0)):
			offsetWidth = 2
		}
		return array.HeaderSize + 4 + (arr.Length()+1)*offsetWidth + totalBytes
	default:
		return array.HeaderSize + arr.Length()*uint64(array.PTypeForType[T]().ByteWidth())
	}
}

func sampleCountApproxOnePercent(length uint64) uint64 {
	if length == 0 {
		return minSampleRuns
	}
	approx := (length / 100) / sampleWindow
	if approx == 0 {
		approx = minSampleRuns
	}
	if approx%16 != 0 {
		approx += 16 - (approx % 16)
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

func sampleArray[T Integer | Float | String](arr array.Array[T]) array.ArrayCore[T] {
	length := arr.Length()
	sampleRuns := sampleCountApproxOnePercent(length)
	totalSample := uint64(sampleWindow) * sampleRuns
	if totalSample >= length {
		return arr
	}

	partitions := partitionIndices(length, sampleRuns)
	chunks := make([]array.Array[T], 0, len(partitions))
	rng := uint64(sampleSeed)

	for _, part := range partitions {
		partLen := part.end - part.start
		if partLen == 0 {
			continue
		}
		take := uint64(sampleWindow)
		if partLen < take {
			take = partLen
		}
		rng = rng*sampleLCGMul + sampleLCGInc
		offset := uint64(0)
		if partLen > take {
			offset = (rng >> 33) % (partLen - take + 1)
		}
		start := part.start + offset
		chunk, err := arr.Slice(start, start+take)
		if err != nil {
			continue
		}
		chunks = append(chunks, chunk)
	}
	if len(chunks) == 0 {
		return arr
	}
	return newSampledArray(chunks)
}

func buildArray[T Integer | Float | String](data []T) array.Array[T] {
	var zero T
	switch any(zero).(type) {
	case int8:
		return any(array.NewPrimitivesUnsafe(any(data).([]int8))).(array.Array[T])
	case int16:
		return any(array.NewPrimitivesUnsafe(any(data).([]int16))).(array.Array[T])
	case int32:
		return any(array.NewPrimitivesUnsafe(any(data).([]int32))).(array.Array[T])
	case int64:
		return any(array.NewPrimitivesUnsafe(any(data).([]int64))).(array.Array[T])
	case uint8:
		return any(array.NewPrimitivesUnsafe(any(data).([]uint8))).(array.Array[T])
	case uint16:
		return any(array.NewPrimitivesUnsafe(any(data).([]uint16))).(array.Array[T])
	case uint32:
		return any(array.NewPrimitivesUnsafe(any(data).([]uint32))).(array.Array[T])
	case uint64:
		return any(array.NewPrimitivesUnsafe(any(data).([]uint64))).(array.Array[T])
	case float32:
		return any(array.NewPrimitivesUnsafe(any(data).([]float32))).(array.Array[T])
	case float64:
		return any(array.NewPrimitivesUnsafe(any(data).([]float64))).(array.Array[T])
	case string:
		return any(array.NewStrings(any(data).([]string))).(array.Array[T])
	default:
		return nil
	}
}
