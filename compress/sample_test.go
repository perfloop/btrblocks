package compress

import (
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

func TestSampleArrayBuildsRangedView(t *testing.T) {
	values := make([]uint32, 4096)
	for i := range values {
		values[i] = uint32(i)
	}

	sampled := sampleArray(array.NewPrimitivesUnsafe(values))
	ranged, ok := sampled.(*sampledArray[uint32])
	require.True(t, ok)
	require.Equal(t, uint64(sampleWindow)*sampleCountApproxOnePercent(uint64(len(values))), ranged.Length())
	require.True(t, len(ranged.ranges) > 1)

	// Each selected range is contiguous in the source array.
	for _, selected := range ranged.ranges {
		for i := selected.start; i+1 < selected.end; i++ {
			require.Equal(t, ranged.source.ValueAt(i)+1, ranged.source.ValueAt(i+1))
		}
	}

	// ValueAt works across range boundaries.
	for i := range ranged.Length() {
		_ = ranged.ValueAt(i) // must not panic
	}

	// rawBinarySize matches a materialized array's BinarySize.
	materialized, err := array.MaterializePrimitiveSlice(ranged, 0, ranged.Length())
	require.NoError(t, err)
	require.Equal(t, materialized.BinarySize(), primitiveRawBinarySize(ranged))
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

	sampled := sampleArray(mustStrings(t, values))
	ranged, ok := sampled.(*sampledArray[string])
	require.True(t, ok)

	materialized, err := array.MaterializeStringSlice(ranged, 0, ranged.Length())
	require.NoError(t, err)
	require.Equal(t, materialized.BinarySize(), stringRawBinarySize(ranged))
}
