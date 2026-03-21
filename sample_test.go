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

	for _, chunk := range chunked.chunks {
		for i := uint64(1); i < chunk.Length(); i++ {
			require.Equal(t, chunk.ValueAt(i-1)+1, chunk.ValueAt(i))
		}
	}

	decoded := make([]uint32, chunked.Length())
	chunked.CopyTo(decoded)
	require.Equal(t, decoded[0]+1, decoded[1])
}
