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

type noDecodeCodec[T Integer | Float | String] struct {
	spyCodec[T]
}

func (c *noDecodeCodec[T]) Decode(dst []T) error {
	c.decodeCalls++
	panic("Decode called")
}

type noValueAtCodec[T Integer | Float | String] struct {
	spyCodec[T]
}

func (c *noValueAtCodec[T]) ValueAt(offset uint64) (T, error) {
	c.valueAtCalls++
	panic("ValueAt called")
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

func TestNestedDecodeUsesBoundedChildScratch(t *testing.T) {
	t.Run("dict indices", func(t *testing.T) {
		values := &spyCodec[int64]{data: []int64{10, 20}, pType: PTypeInt64}
		indices := &noDecodeCodec[uint8]{spyCodec: spyCodec[uint8]{data: []uint8{1, 0, 1, 1}, pType: PTypeUint8}}
		codec := &DictCodec[int64, uint8]{values: values, indices: indices}
		dst := make([]int64, 4)

		require.NoError(t, codec.Decode(dst))
		require.Equal(t, []int64{20, 10, 20, 20}, dst)
		require.Equal(t, 1, values.decodeCalls)
		require.Zero(t, indices.decodeCalls)
	})

	t.Run("runend children", func(t *testing.T) {
		runs := &noValueAtCodec[uint64]{spyCodec: spyCodec[uint64]{data: []uint64{5, 8, 13}, pType: PTypeUint64}}
		ends := &noValueAtCodec[uint8]{spyCodec: spyCodec[uint8]{data: []uint8{3, 5}, pType: PTypeUint8}}
		codec := &RunendCodec[uint64, uint8]{length: 8, runs: runs, ends: ends}
		dst := make([]uint64, 8)

		require.NoError(t, codec.Decode(dst))
		require.Equal(t, []uint64{5, 5, 5, 8, 8, 13, 13, 13}, dst)
		require.Equal(t, 1, runs.decodeCalls)
		require.Equal(t, 1, ends.decodeCalls)
	})

	t.Run("zigzag child", func(t *testing.T) {
		child := &noValueAtCodec[uint8]{spyCodec: spyCodec[uint8]{data: []uint8{1, 0, 2, 3}, pType: PTypeUint8}}
		codec := &ZigzagCodec[int64, uint8]{data: child}
		dst := make([]int64, 4)

		require.NoError(t, codec.Decode(dst))
		require.Equal(t, []int64{-1, 0, 1, -2}, dst)
		require.Equal(t, 1, child.decodeCalls)
	})
}

func TestNestedDecodeFallsBackToValueAtWhenLarge(t *testing.T) {
	t.Run("runend children", func(t *testing.T) {
		runCount := int(maxDecodeScratchBytes/4) + 2
		runsData := make([]uint64, runCount)
		endsData := make([]uint32, runCount-1)
		for i := range runsData {
			runsData[i] = uint64(i)
			if i < len(endsData) {
				endsData[i] = uint32(i + 1)
			}
		}
		runs := &noDecodeCodec[uint64]{spyCodec: spyCodec[uint64]{data: runsData, pType: PTypeUint64}}
		ends := &noDecodeCodec[uint32]{spyCodec: spyCodec[uint32]{data: endsData, pType: PTypeUint32}}
		codec := &RunendCodec[uint64, uint32]{length: uint64(runCount), runs: runs, ends: ends}
		dst := make([]uint64, runCount)

		require.NoError(t, codec.Decode(dst))
		require.Equal(t, runsData, dst)
		require.Zero(t, runs.decodeCalls)
		require.Zero(t, ends.decodeCalls)
		require.NotZero(t, runs.valueAtCalls)
		require.NotZero(t, ends.valueAtCalls)
	})

	t.Run("zigzag child", func(t *testing.T) {
		length := int(maxDecodeScratchBytes/8) + 1
		data := make([]uint64, length)
		for i := range data {
			data[i] = uint64(i)
		}
		child := &noDecodeCodec[uint64]{spyCodec: spyCodec[uint64]{data: data, pType: PTypeUint64}}
		codec := &ZigzagCodec[int64, uint64]{data: child}
		dst := make([]int64, length)

		require.NoError(t, codec.Decode(dst))
		require.Zero(t, child.decodeCalls)
		require.NotZero(t, child.valueAtCalls)
	})
}

func TestValidateDictCodecAvoidsChildBulkDecode(t *testing.T) {
	values := NewRawCodec(array.NewPrimitivesUnsafe([]int64{10, 20}))
	indices := &noDecodeCodec[uint8]{spyCodec: spyCodec[uint8]{data: []uint8{0, 1, 1}, pType: PTypeUint8}}

	require.NoError(t, validateDictCodec[int64](3, values, indices))
	require.Zero(t, indices.decodeCalls)
}

func TestDictDecodeFallsBackToValueAtForLargeCardinality(t *testing.T) {
	valueCount := int(maxDecodeScratchBytes/8) + 1
	valuesData := make([]int64, valueCount)
	for i := range valuesData {
		valuesData[i] = int64(i)
	}
	values := &noDecodeCodec[int64]{spyCodec: spyCodec[int64]{data: valuesData, pType: PTypeInt64}}
	indices := &spyCodec[uint32]{data: []uint32{1, 3, 5, 7}, pType: PTypeUint32}
	codec := &DictCodec[int64, uint32]{values: values, indices: indices}
	dst := make([]int64, 4)

	require.NoError(t, codec.Decode(dst))
	require.Equal(t, []int64{1, 3, 5, 7}, dst)
	require.Zero(t, values.decodeCalls)
}

func TestDictDecodeReusesValuesScratch(t *testing.T) {
	values := &spyCodec[int64]{data: []int64{10, 20, 30}, pType: PTypeInt64}
	indices := &spyCodec[uint8]{data: []uint8{2, 1, 0}, pType: PTypeUint8}
	codec := &DictCodec[int64, uint8]{values: values, indices: indices}
	dst := make([]int64, 3)
	scratch := make([]int64, 0, 3)

	reused, err := codec.decode(dst, scratch)
	require.NoError(t, err)
	require.Equal(t, []int64{30, 20, 10}, dst)
	require.Equal(t, 1, values.decodeCalls)
	require.Equal(t, 3, len(reused))
	require.Equal(t, cap(scratch), cap(reused))
	require.Equal(t, &scratch[:1][0], &reused[:1][0])
}
