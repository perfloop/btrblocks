package btrblocks

import "github.com/axiomhq/btrblocks/array"

const (
	// sampleThreshold is the minimum array length before sampling kicks in.
	// Below this, selectBest evaluates all builders on the full data.
	sampleThreshold = 2048

	sampleChunks   = 16
	samplePerChunk = 64
	sampleSize     = sampleChunks * samplePerChunk // 1024
)

// sampleArray returns a stratified sample of arr: sampleChunks evenly-spaced
// chunks of samplePerChunk consecutive elements each. The caller must ensure
// arr.Length() >= sampleThreshold.
func sampleArray[T Integer | Float | String](arr array.Array[T]) array.Array[T] {
	n := arr.Length()
	chunkStride := n / sampleChunks
	sampled := make([]T, 0, sampleSize)
	for chunk := range uint64(sampleChunks) {
		start := chunk * chunkStride
		for j := range uint64(samplePerChunk) {
			sampled = append(sampled, arr.ValueAt(start+j))
		}
	}
	return buildArray(sampled)
}

// sampleHints holds lightweight statistics computed from a sample for
// stats-based builder rejection, matching Vortex's Level 1 filtering.
type sampleHints struct {
	distinctRatio float64 // distinctCount / length
	avgRunLength  float64 // length / runCount
}

// shouldSkip returns true if a builder of the given kind should be skipped
// based on sample statistics.
func (h sampleHints) shouldSkip(kind CodecType) bool {
	switch kind {
	case CodecTypeDict:
		// Vortex: skip if >50% distinct values.
		return h.distinctRatio > 0.5
	case CodecTypeRunend:
		// Skip if average run length < 2 (almost no runs).
		return h.avgRunLength < 2.0
	default:
		return false
	}
}

// computeSampleHints scans a sample array once to compute lightweight stats.
func computeSampleHints[T Integer | Float | String](arr array.Array[T]) sampleHints {
	n := arr.Length()
	if n == 0 {
		return sampleHints{}
	}
	distinct := make(map[any]struct{}, 256)
	runs := uint64(1)
	prev := any(arr.ValueAt(0))
	distinct[prev] = struct{}{}
	for i := uint64(1); i < n; i++ {
		v := any(arr.ValueAt(i))
		distinct[v] = struct{}{}
		if v != prev {
			runs++
			prev = v
		}
	}
	return sampleHints{
		distinctRatio: float64(len(distinct)) / float64(n),
		avgRunLength:  float64(n) / float64(runs),
	}
}

// buildArray wraps a []T into the appropriate array.Array[T].
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
