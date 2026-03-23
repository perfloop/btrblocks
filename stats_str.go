package btrblocks

import "github.com/axiomhq/btrblocks/array"

// stringStats mirrors the narrower Rust string planner stats: it keeps the
// source array plus an approximate distinct-count estimate and leaves constant
// and run detection to scheme-specific checks.
type stringStats struct {
	src                    array.Array[string]
	estimatedDistinctCount uint64
}

// stringDistinctKey mirrors the Rust reference's cheap string-cardinality estimate:
// strings are grouped by byte length and the first 8 bytes of content instead of
// hashing the full string payload.
type stringDistinctKey struct {
	length uint64
	prefix [8]byte
}

func (s stringStats) Source() array.Array[string] {
	return s.src
}

func (s stringStats) Sample(ctx planContext) array.Array[string] {
	if ctx.isSample {
		return s.src
	}
	return sampleArray(s.src)
}

// computeStringStats mirrors the Rust string planner stats by only keeping the
// source array plus an approximate distinct estimate based on string length and
// the first 8 bytes.
func computeStringStats(arr array.Array[string]) stringStats {
	n := arr.Length()
	if n == 0 {
		return stringStats{src: arr}
	}

	distinct := make(map[stringDistinctKey]struct{}, 256)
	for i := uint64(0); i < n; i++ {
		v := arr.ValueAt(i)
		distinct[stringKey(v)] = struct{}{}
	}

	return stringStats{
		src:                    arr,
		estimatedDistinctCount: uint64(len(distinct)),
	}
}

func stringKey(value string) stringDistinctKey {
	key := stringDistinctKey{length: uint64(len(value))}
	for i := 0; i < len(key.prefix) && i < len(value); i++ {
		key.prefix[i] = value[i]
	}
	return key
}
