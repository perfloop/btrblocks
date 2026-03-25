package btrblocks

import (
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

func TestSampleArrayBuildsChunkedSlices(t *testing.T) {
	values := make([]uint32, 4096)
	for i := range values {
		values[i] = uint32(i)
	}

	sampled := sampleArray(array.NewPrimitivesUnsafe(values))
	chunked, ok := sampled.(*sampledArray[uint32])
	require.True(t, ok)
	require.Equal(t, uint64(sampleWindow)*sampleCountApproxOnePercent(uint64(len(values))), chunked.Length())
	require.Greater(t, len(chunked.chunks), 1)

	// Each chunk is a contiguous slice of the source array.
	for _, chunk := range chunked.chunks {
		for i := uint64(1); i < chunk.Length(); i++ {
			require.Equal(t, chunk.ValueAt(i-1)+1, chunk.ValueAt(i))
		}
	}

	// ValueAt works across chunk boundaries.
	for i := uint64(0); i < chunked.Length(); i++ {
		_ = chunked.ValueAt(i) // must not panic
	}

	// rawBinarySize matches a materialized array's BinarySize.
	materialized, err := materializeSlice[uint32](chunked, 0, chunked.Length())
	require.NoError(t, err)
	require.Equal(t, materialized.BinarySize(), rawBinarySize[uint32](chunked))
}

func TestSampleArrayBinarySizeMatchesMaterializedStrings(t *testing.T) {
	values := make([]string, 4096)
	for i := range values {
		if i%3 == 0 {
			values[i] = "alpha"
			continue
		}
		if i%3 == 1 {
			values[i] = "bravo-bravo"
			continue
		}
		values[i] = "charlie-charlie-charlie"
	}

	sampled := sampleArray(array.NewStrings(values))
	chunked, ok := sampled.(*sampledArray[string])
	require.True(t, ok)

	materialized, err := materializeSlice[string](chunked, 0, chunked.Length())
	require.NoError(t, err)
	require.Equal(t, materialized.BinarySize(), rawBinarySize[string](chunked))
}
