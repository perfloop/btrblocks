package btrblocks

import (
	"io"

	"github.com/axiomhq/btrblocks/array"
)

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
type sampledArray[T Integer | Float | String] struct {
	pType   PType
	length  uint64
	offsets []uint64
	chunks  []array.Array[T]
}

func newSampledArray[T Integer | Float | String](chunks []array.Array[T]) array.Array[T] {
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
	chunkIdx := 0
	for a.offsets[chunkIdx+1] <= offset {
		chunkIdx++
	}
	return a.chunks[chunkIdx].ValueAt(offset - a.offsets[chunkIdx])
}

func (a *sampledArray[T]) CopyTo(dst []T) {
	pos := 0
	for _, chunk := range a.chunks {
		end := pos + int(chunk.Length())
		chunk.CopyTo(dst[pos:end])
		pos = end
	}
}

func (a *sampledArray[T]) Slice(start, end uint64) (array.Array[T], error) {
	return materializeSlice[T](a, start, end)
}

func (a *sampledArray[T]) BinarySize() uint64 {
	values, err := materializeSlice[T](a, 0, a.length)
	if err != nil {
		panic(err)
	}
	return values.BinarySize()
}

func (a *sampledArray[T]) Length() uint64 {
	return a.length
}

func (a *sampledArray[T]) PType() PType {
	return a.pType
}

func (a *sampledArray[T]) WriteTo(w io.Writer) (int64, error) {
	values, err := materializeSlice[T](a, 0, a.length)
	if err != nil {
		return 0, err
	}
	return values.WriteTo(w)
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

func sampleArray[T Integer | Float | String](arr array.Array[T]) array.Array[T] {
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
			panic(err)
		}
		chunks = append(chunks, chunk)
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
