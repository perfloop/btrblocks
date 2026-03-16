package btrblocks

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"strconv"
	"testing"

	"github.com/axiomhq/btrblocks/array"
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

type noCopyArray[T Integer | Float | String] struct {
	base   array.Array[T]
	copied *bool
}

func (a noCopyArray[T]) WriteTo(w io.Writer) (int64, error) { return a.base.WriteTo(w) }
func (a noCopyArray[T]) ValueAt(offset uint64) T            { return a.base.ValueAt(offset) }
func (a noCopyArray[T]) BinarySize() uint64                 { return a.base.BinarySize() }
func (a noCopyArray[T]) Length() uint64                     { return a.base.Length() }
func (a noCopyArray[T]) PType() PType                       { return a.base.PType() }

func (a noCopyArray[T]) CopyTo(dst []T) {
	*a.copied = true
	panic("CopyTo called")
}

type spyCodec[T Integer | Float | String] struct {
	data         []T
	pType        PType
	decodeCalls  int
	valueAtCalls int
}

func (c *spyCodec[T]) Children() []Scheme               { return nil }
func (c *spyCodec[T]) WriteTo(io.Writer) (int64, error) { return 0, nil }
func (c *spyCodec[T]) BinarySize() uint64               { return 0 }
func (c *spyCodec[T]) Length() uint64                   { return uint64(len(c.data)) }
func (c *spyCodec[T]) PType() PType                     { return c.pType }
func (c *spyCodec[T]) ValueAt(offset uint64) (T, error) {
	c.valueAtCalls++
	if offset >= uint64(len(c.data)) {
		var zero T
		return zero, errOffsetOutOfRange
	}
	return c.data[offset], nil
}

func (c *spyCodec[T]) Decode(dst []T) error {
	c.decodeCalls++
	if err := validateDecodeLength(uint64(len(c.data)), len(dst)); err != nil {
		return err
	}
	copy(dst, c.data)
	return nil
}


func TestCompressPathsAvoidCopyTo(t *testing.T) {
	t.Run("signed integer", func(t *testing.T) {
		copied := false
		arr := noCopyArray[int64]{
			base:   array.NewPrimitivesUnsafe([]int64{-3, -2, -1, 0, 1, 2, 3}),
			copied: &copied,
		}
		codec := CompressInteger[int64](arr, defaultDepth)
		require.NotNil(t, codec)
		require.False(t, copied)
	})

	t.Run("unsigned integer", func(t *testing.T) {
		copied := false
		arr := noCopyArray[uint64]{
			base:   array.NewPrimitivesUnsafe([]uint64{0, 1, 3, 7, 15, 31}),
			copied: &copied,
		}
		codec := CompressInteger[uint64](arr, defaultDepth)
		require.NotNil(t, codec)
		require.False(t, copied)
	})

	t.Run("float", func(t *testing.T) {
		copied := false
		arr := noCopyArray[float64]{
			base:   array.NewPrimitivesUnsafe([]float64{1.5, 1.5, 2.5, 2.5}),
			copied: &copied,
		}
		codec := CompressFloat[float64](arr, defaultDepth)
		require.NotNil(t, codec)
		require.False(t, copied)
	})

	t.Run("string", func(t *testing.T) {
		copied := false
		arr := noCopyArray[string]{
			base:   array.NewStrings([]string{"aa", "aa", "bb", "bb"}),
			copied: &copied,
		}
		codec := CompressString(arr, defaultDepth)
		require.NotNil(t, codec)
		require.False(t, copied)
	})
}

func TestNestedDecodeBulkDecodesChildren(t *testing.T) {
	t.Run("dict", func(t *testing.T) {
		values := &spyCodec[int64]{data: []int64{10, 20}, pType: PTypeInt64}
		indices := &spyCodec[uint8]{data: []uint8{1, 0, 1, 1}, pType: PTypeUint8}
		codec := &DictCodec[int64, uint8]{values: values, indices: indices}
		dst := make([]int64, 4)

		require.NoError(t, codec.Decode(dst))
		require.Equal(t, []int64{20, 10, 20, 20}, dst)
		require.Equal(t, 1, values.decodeCalls)
		require.Equal(t, 1, indices.decodeCalls)
	})

	t.Run("runend", func(t *testing.T) {
		runs := &spyCodec[uint64]{data: []uint64{5, 8, 13}, pType: PTypeUint64}
		ends := &spyCodec[uint8]{data: []uint8{3, 5}, pType: PTypeUint8}
		codec := &RunendCodec[uint64, uint8]{length: 8, runs: runs, ends: ends}
		dst := make([]uint64, 8)

		require.NoError(t, codec.Decode(dst))
		require.Equal(t, []uint64{5, 5, 5, 8, 8, 13, 13, 13}, dst)
		require.Equal(t, 1, runs.decodeCalls)
		require.Equal(t, 1, ends.decodeCalls)
	})

	t.Run("zigzag", func(t *testing.T) {
		child := &spyCodec[uint8]{data: []uint8{1, 0, 2, 3}, pType: PTypeUint8}
		codec := &ZigzagCodec[int64, uint8]{data: child}
		dst := make([]int64, 4)

		require.NoError(t, codec.Decode(dst))
		require.Equal(t, []int64{-1, 0, 1, -2}, dst)
		require.Equal(t, 1, child.decodeCalls)
	})
}
