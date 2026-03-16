package btrblocks

import (
	"fmt"
	"testing"

	"github.com/axiomhq/btrblocks/array"
)

func BenchmarkSelectBest(b *testing.B) {
	for _, size := range []int{64_000, 1_000_000} {
		// Low-cardinality interleaved data — Dict should win.
		numbers := []int32{0, 123400, 617000, 1234000, 12340000, 37020000}
		data := make([]int32, size)
		for i := range data {
			data[i] = numbers[(i*7+3)%len(numbers)]
		}
		arr := array.NewPrimitivesUnsafe(data)
		label := fmt.Sprintf("%dK", size/1000)

		b.Run("sampled/"+label, func(b *testing.B) {
			builders := append(integerBuilders[int32](), signedIntegerBuilders[int32]()...)
			b.ResetTimer()
			for range b.N {
				selectBest(arr, defaultDepth, builders)
			}
		})

		b.Run("full/"+label, func(b *testing.B) {
			builders := append(integerBuilders[int32](), signedIntegerBuilders[int32]()...)
			b.ResetTimer()
			for range b.N {
				selectBestAll(arr, defaultDepth, builders)
			}
		})
	}
}
