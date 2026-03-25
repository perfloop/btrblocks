package btrblocks

import (
	"bytes"
	"fmt"
	"io"
	"testing"

	"github.com/axiomhq/btrblocks/array"
)

// --- patches.Slice benchmark ---

func BenchmarkPatchesSlice(b *testing.B) {
	n := 100_000
	values := make([]uint32, n)
	for i := range values {
		values[i] = uint32(i % 16)
	}
	for i := 0; i < n; i += 20 {
		values[i] = 1 << 20
	}

	encoded, err := buildBitPackedArray(array.NewPrimitivesUnsafe(values), newPlanContext(Options{}))
	if err != nil {
		b.Fatal(err)
	}
	bp := encoded.(*bitPackedArray[uint32, uint64])
	if bp.patches == nil {
		b.Fatal("expected patches")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := bp.patches.Slice(1000, 50000)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// --- ALPRD DecompressInto benchmark ---

func makeALPRDWithPatches(n int) EncodedArray[float64] {
	values := make([]float64, n)
	for i := range values {
		values[i] = 1.0 + float64(i)*1e-10
	}
	for i := 0; i < n; i += 50 {
		values[i] = 1e100
	}
	codec, err := buildALPRDArray[float64](array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}))
	if err != nil {
		codec2, err2 := Compress(buildArray(values), Options{})
		if err2 != nil {
			panic(err2)
		}
		return codec2
	}
	return codec
}

func BenchmarkALPRDDecompressIntoPatched(b *testing.B) {
	for _, n := range []int{10_000, 100_000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			benchDecompressInto(b, makeALPRDWithPatches(n))
		})
	}
}

// --- writeVirtualArray benchmark (used by FoR, ZigZag, ALP) ---

func BenchmarkWriteVirtualArray(b *testing.B) {
	for _, n := range []int{10_000, 100_000} {
		values := make([]uint32, n)
		for i := range values {
			values[i] = 1_000_000 + uint32(i%64)
		}
		codec, err := buildFoRArray(array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}))
		if err != nil {
			b.Fatal(err)
		}

		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := codec.WriteTo(io.Discard); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// --- Small-array compress benchmark (measures per-call overhead) ---

func BenchmarkCompressSmall(b *testing.B) {
	b.Run("uint32", func(b *testing.B) {
		values := make([]uint32, 64)
		for i := range values {
			values[i] = uint32(i)
		}
		arr := buildArray(values)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := Compress(arr, Options{}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("float64", func(b *testing.B) {
		values := make([]float64, 64)
		for i := range values {
			values[i] = float64(i) * 1.1
		}
		arr := buildArray(values)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := Compress(arr, Options{}); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// --- writeConstElementArray benchmark ---

func BenchmarkWriteConstElementArray(b *testing.B) {
	values := make([]uint64, 10_000)
	for i := range values {
		values[i] = 42
	}
	codec, err := Compress(array.NewPrimitivesUnsafe(values), Options{})
	if err != nil {
		b.Fatal(err)
	}

	var buf bytes.Buffer
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		if _, err := codec.WriteTo(&buf); err != nil {
			b.Fatal(err)
		}
	}
}
