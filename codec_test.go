package btrblocks

import (
	"bytes"
	"fmt"
	"math"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const largeCorpusSize = 1 << 20

func assertCodecMetadata[T Integer | Float | String](t *testing.T, codec Codec[T], wantLen int, wantPType PType, wantChildren int) {
	t.Helper()

	require.NotNil(t, codec)
	require.Equal(t, uint64(wantLen), codec.Length())
	require.Equal(t, wantPType, codec.PType())
	require.Len(t, codec.Children(), wantChildren)
}

func assertCodecRoundTrip[T Integer | Float | String](t *testing.T, codec Codec[T], data []T) {
	t.Helper()

	for i, want := range data {
		got, err := codec.ValueAt(uint64(i))
		require.NoError(t, err, "ValueAt(%d)", i)
		assertValueEqual(t, got, want, i)
	}

	_, err := codec.ValueAt(codec.Length())
	require.ErrorIs(t, err, errOffsetOutOfRange)

	var buf bytes.Buffer
	n, err := codec.WriteTo(&buf)
	require.NoError(t, err)
	require.Equal(t, int64(codec.BinarySize()), n)
	assertCodecHeader(t, codec, &buf)
}

func assertValueEqual[T Integer | Float | String](t *testing.T, got, want T, offset int) {
	t.Helper()

	switch w := any(want).(type) {
	case float32:
		assert.Equal(t, math.Float32bits(w), math.Float32bits(any(got).(float32)), "ValueAt(%d)", offset)
	case float64:
		assert.Equal(t, math.Float64bits(w), math.Float64bits(any(got).(float64)), "ValueAt(%d)", offset)
	default:
		assert.Equal(t, want, got, "ValueAt(%d)", offset)
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

func assertCodecHeader[T Integer | Float | String](t *testing.T, codec Codec[T], buf *bytes.Buffer) {
	t.Helper()

	header, err := readHeader(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	require.Equal(t, uint8(1), header.Version)
	require.Equal(t, codec.PType(), header.ElemType)
	require.Equal(t, uint8(len(codec.Children())), header.ChildCount)
	require.Equal(t, codec.Length(), header.Length)
	require.LessOrEqual(t, uint64(headerSize)+header.BodySize, codec.BinarySize())
}

type childCodecMetadata interface {
	Length() uint64
	PType() PType
}

func requireChildCodecMetadata(t *testing.T, scheme Scheme) childCodecMetadata {
	t.Helper()

	child, ok := scheme.(childCodecMetadata)
	require.True(t, ok, "child %T does not expose codec metadata", scheme)
	return child
}
