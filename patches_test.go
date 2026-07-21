package btrblocks

import (
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

func TestPatchesRejectCountBeyondLogicalLength(t *testing.T) {
	_, err := newPatches[uint32, uint8](
		1,
		0,
		&constArray[uint8]{denseRows: 2, body: array.NewPrimitivesUnsafe([]uint8{0})},
		&constArray[uint32]{denseRows: 2, body: array.NewPrimitivesUnsafe([]uint32{1})},
	)
	require.ErrorContains(t, err, "patch count 2 exceeds logical length 1")
}

func BenchmarkPatchesSlice(b *testing.B) {
	n := 100_000
	values := make([]uint32, n)
	for i := range values {
		values[i] = uint32(i % 16)
	}
	for i := 0; i < n; i += 20 {
		values[i] = 1 << 20
	}

	encoded, err := encodeBitpack(array.NewPrimitivesUnsafe(values))
	if err != nil {
		b.Fatal(err)
	}
	bp := encoded.(*bitPackedArray[uint32, uint64])
	if bp.patches == nil {
		b.Fatal("expected patches")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_, err := bp.patches.Slice(1000, 50000)
		if err != nil {
			b.Fatal(err)
		}
	}
}
