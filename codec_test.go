package btrblocks

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"testing"
)

const largeCorpusSize = 1 << 20

func assertCodecMetadata[T Integer | Float | String](t *testing.T, codec Codec[T], wantLen int, wantPType PType, wantChildren int) {
	t.Helper()

	if codec == nil {
		t.Fatal("codec is nil")
	}
	if got := codec.Length(); got != uint64(wantLen) {
		t.Fatalf("Length() = %d, want %d", got, wantLen)
	}
	if got := codec.PType(); got != wantPType {
		t.Fatalf("PType() = %v, want %v", got, wantPType)
	}
	if got := len(codec.Children()); got != wantChildren {
		t.Fatalf("len(Children()) = %d, want %d", got, wantChildren)
	}
}

func assertCodecRoundTrip[T Integer | Float | String](t *testing.T, codec Codec[T], data []T) {
	t.Helper()

	for i, want := range data {
		got, err := codec.ValueAt(uint64(i))
		if err != nil {
			t.Fatalf("ValueAt(%d) returned error: %v", i, err)
		}
		assertValueEqual(t, got, want, i)
	}

	if _, err := codec.ValueAt(codec.Length()); err != errOffsetOutOfRange {
		t.Fatalf("ValueAt(%d) error = %v, want %v", codec.Length(), err, errOffsetOutOfRange)
	}

	n, err := codec.WriteTo(io.Discard)
	if err != nil {
		t.Fatalf("WriteTo() returned error: %v", err)
	}
	if want := int64(codec.BinarySize()); n != want {
		t.Fatalf("WriteTo() bytes = %d, want %d", n, want)
	}
}

func assertValueEqual[T Integer | Float | String](t *testing.T, got, want T, offset int) {
	t.Helper()

	switch w := any(want).(type) {
	case float32:
		if math.Float32bits(any(got).(float32)) != math.Float32bits(w) {
			t.Fatalf("ValueAt(%d) = %v, want %v", offset, got, want)
		}
	case float64:
		if math.Float64bits(any(got).(float64)) != math.Float64bits(w) {
			t.Fatalf("ValueAt(%d) = %v, want %v", offset, got, want)
		}
	default:
		if got != want {
			t.Fatalf("ValueAt(%d) = %v, want %v", offset, got, want)
		}
	}
}

func makeConstantCorpus[T any](n int, value T) []T {
	out := make([]T, n)
	for i := range out {
		out[i] = value
	}
	return out
}

func makeRampInt64Corpus(n int) []int64 {
	out := make([]int64, n)
	for i := range out {
		out[i] = int64(i) - int64(n/2)
	}
	return out
}

func makeUniqueFloat64Corpus(n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = float64(i)*0.5 + float64(i%17)/17
	}
	return out
}

func makeLowCardinalityUint64Corpus(n, cardinality int) []uint64 {
	if cardinality < 1 {
		cardinality = 1
	}
	out := make([]uint64, n)
	for i := range out {
		out[i] = uint64((i*17 + i/31) % cardinality)
	}
	return out
}

func makeRunUint64Corpus(n, runLength int) []uint64 {
	if runLength < 1 {
		runLength = 1
	}
	out := make([]uint64, n)
	for i := range out {
		out[i] = uint64(i / runLength)
	}
	return out
}

func makeLowCardinalityStringCorpus(n, cardinality int) []string {
	if cardinality < 1 {
		cardinality = 1
	}
	dict := make([]string, cardinality)
	for i := range dict {
		dict[i] = "value-" + strconv.Itoa(i)
	}
	out := make([]string, n)
	for i := range out {
		out[i] = dict[(i*17+i/31)%cardinality]
	}
	return out
}

func benchmarkValueAtLoop[T Integer | Float | String](b *testing.B, codec Codec[T], size int) {
	b.Helper()
	b.ReportAllocs()
	b.ResetTimer()

	var sink T
	for i := 0; i < b.N; i++ {
		value, err := codec.ValueAt(uint64(i & (size - 1)))
		if err != nil {
			b.Fatalf("ValueAt() returned error: %v", err)
		}
		sink = value
	}

	_ = sink
}

func benchmarkBuildLoop[T Integer | Float | String](b *testing.B, name string, builder func([]T) (Codec[T], error), data []T) {
	b.Helper()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		codec, err := builder(data)
		if err != nil {
			b.Fatalf("%s build failed: %v", name, err)
		}
		if codec == nil {
			b.Fatalf("%s build returned nil codec", name)
		}
	}
}

func describeCodec(codec any) string {
	return fmt.Sprintf("%T", codec)
}
