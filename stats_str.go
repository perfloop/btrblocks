package btrblocks

import "github.com/axiomhq/btrblocks/array"

type stringStats struct {
	base                   baseStats[string]
	estimatedDistinctCount uint64
}

func (s stringStats) Source() array.Array[string] {
	return s.base.Source()
}

func (s stringStats) Sample(ctx planContext) array.ArrayCore[string] {
	return s.base.Sample(ctx)
}

// computeStringStats estimates string cardinality using byte length + first 8
// bytes as a cheap distinct key (mirrors the Rust reference). Also computes
// isConst and avgRunLength so these don't need to be re-scanned in Schemes().
func computeStringStats(arr array.Array[string]) stringStats {
	n := arr.Length()
	if n == 0 {
		return stringStats{base: baseStats[string]{src: arr, cached: arr}}
	}

	type key struct {
		length uint64
		prefix [8]byte
	}

	distinct := make(map[key]struct{}, 256)
	runs := uint64(1)
	isConst := true
	first := arr.ValueAt(0)
	prev := first

	var k0 key
	k0.length = uint64(len(first))
	for j := 0; j < len(k0.prefix) && j < len(first); j++ {
		k0.prefix[j] = first[j]
	}
	distinct[k0] = struct{}{}

	for i := uint64(1); i < n; i++ {
		v := arr.ValueAt(i)
		var k key
		k.length = uint64(len(v))
		for j := 0; j < len(k.prefix) && j < len(v); j++ {
			k.prefix[j] = v[j]
		}
		distinct[k] = struct{}{}

		if v != prev {
			runs++
			prev = v
			if isConst {
				isConst = false
			}
		}
	}

	return stringStats{
		base: baseStats[string]{
			src:           arr,
			cached:        sampleArray(arr),
			isConst:       isConst,
			distinctCount: uint64(len(distinct)),
			avgRunLength:  float64(n) / float64(runs),
		},
		estimatedDistinctCount: uint64(len(distinct)),
	}
}
