package btrblocks

import (
	"io"
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

	materialized, err := materializeSlice[uint32](chunked, 0, chunked.Length())
	require.NoError(t, err)
	require.Equal(t, materialized.BinarySize(), chunked.BinarySize())

	require.Panics(t, func() {
		chunked.CopyTo(make([]uint32, chunked.Length()))
	})
	require.Panics(t, func() {
		_, _ = chunked.Slice(0, 1)
	})
	require.Panics(t, func() {
		_ = chunked.PType()
	})
	require.Panics(t, func() {
		_, _ = chunked.WriteTo(io.Discard)
	})
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
	require.Equal(t, materialized.BinarySize(), chunked.BinarySize())
}
