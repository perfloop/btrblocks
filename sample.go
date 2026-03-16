package btrblocks

import (
	"github.com/axiomhq/btrblocks/array"
)

const (
	// sampleThreshold is the minimum array length before sampling kicks in.
	// Below this, selectBest evaluates all builders on the full data.
	sampleThreshold = 2048

	sampleChunks   = 16
	samplePerChunk = 64
	sampleSize     = sampleChunks * samplePerChunk // 1024

	// LCG parameters for deterministic pseudo-random chunk offsets.
	// Seed matches Vortex's StdRng::seed_from_u64(1234567890).
	sampleSeed   = 1234567890
	sampleLCGMul = 6364136223846793005
	sampleLCGInc = 1442695040888963407
)

// sampleArray returns a stratified sample of arr: sampleChunks evenly-spaced
// chunks of samplePerChunk consecutive elements each, with a deterministic
// pseudo-random offset within each chunk (matching Vortex's seeded RNG
// approach). The caller must ensure arr.Length() >= sampleThreshold.
func sampleArray[T Integer | Float | String](arr array.Array[T]) array.Array[T] {
	var (
		n           = arr.Length()
		chunkStride = n / sampleChunks
		sampled     = make([]T, 0, sampleSize)
		rng         = uint64(sampleSeed)
	)

	for chunk := range uint64(sampleChunks) {
		maxOffset := chunkStride - samplePerChunk
		rng = rng*sampleLCGMul + sampleLCGInc
		offset := uint64(0)
		if maxOffset > 0 {
			offset = (rng >> 33) % maxOffset
		}
		start := chunk*chunkStride + offset
		for j := range uint64(samplePerChunk) {
			sampled = append(sampled, arr.ValueAt(start+j))
		}
	}
	return buildArray(sampled)
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
