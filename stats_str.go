package btrblocks

import "github.com/axiomhq/btrblocks/array"

type stringStats struct {
	src                    array.Array[string]
	estimatedDistinctCount uint64
}

func (s stringStats) Source() array.Array[string] {
	return s.src
}

func (s stringStats) Sample(ctx planContext) array.ArrayCore[string] {
	if ctx.isSample {
		return s.src
	}
	return sampleArray(s.src)
}

// computeStringStats estimates string cardinality using byte length + first 8
// bytes as a cheap distinct key (mirrors the Rust reference).
func computeStringStats(arr array.Array[string]) stringStats {
	n := arr.Length()
	if n == 0 {
		return stringStats{src: arr}
	}

	type key struct {
		length uint64
		prefix [8]byte
	}

	distinct := make(map[key]struct{}, 256)
	for i := uint64(0); i < n; i++ {
		v := arr.ValueAt(i)
		var k key
		k.length = uint64(len(v))
		for j := 0; j < len(k.prefix) && j < len(v); j++ {
			k.prefix[j] = v[j]
		}
		distinct[k] = struct{}{}
	}

	return stringStats{
		src:                    arr,
		estimatedDistinctCount: uint64(len(distinct)),
	}
}
