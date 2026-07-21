package btrblocks_test

import (
	"testing"

	"github.com/axiomhq/btrblocks"
	"github.com/axiomhq/btrblocks/array"
)

func TestCompressionFacade(t *testing.T) {
	want := []int64{10, 12, 14, 16, 18}
	opts := btrblocks.Options{}.
		WithMaxDepth(2).
		WithExcludeInteger(btrblocks.CodecTypeDict)

	encoded, err := btrblocks.SignedArray(array.NewPrimitivesUnsafe(want), opts)
	if err != nil {
		t.Fatalf("SignedArray: %v", err)
	}
	got, err := btrblocks.Decompress(encoded)
	if err != nil {
		t.Fatalf("Decompress: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("decoded length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("decoded[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}
