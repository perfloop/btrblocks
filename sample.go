package btrblocks

import "github.com/axiomhq/btrblocks/array"

const (
	sampleWindow  = 64
	minSampleRuns = 16

	sampleSeed   = 1234567890
	sampleLCGMul = 6364136223846793005
	sampleLCGInc = 1442695040888963407
)

type indexRange struct {
	start uint64
	end   uint64
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
	sampled := make([]T, 0, totalSample)
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
		for i := uint64(0); i < take; i++ {
			sampled = append(sampled, arr.ValueAt(start+i))
		}
	}
	return buildArray(sampled)
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
