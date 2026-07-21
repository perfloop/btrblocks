package codec

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEnterKeepsNoLevelItRejected pins that a depth rejection leaves the shared
// readOptions as it found it. Every node of one decode threads the same pointer
// and only pairs leave with a successful enter, so a level taken on the way out
// would be charged to sibling branches that never nested at all.
func TestEnterKeepsNoLevelItRejected(t *testing.T) {
	opts := newReadOptions(ReadOptions{MaxDepth: 1})
	require.NoError(t, opts.enter())
	require.ErrorContains(t, opts.enter(), "nesting depth exceeds limit 1")
	opts.leave()
	require.Zero(t, opts.depth)
}
