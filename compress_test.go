package btrblocks

import (
	"bytes"
	"io"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

type testCodecUint32 struct {
	kind   CodeType
	length uint64
	size   uint64
}

type testStatsUint32 struct {
	arr array.Array[uint32]
}

type spyCodec[T Integer | Float | String] struct {
	values      []T
	decodeCalls int
	decodeErr   error
}

func (c *testCodecUint32) Kind() CodeType     { return c.kind }
func (c *testCodecUint32) Length() uint64     { return c.length }
func (c *testCodecUint32) PType() PType       { return PTypeUint32 }
func (c *testCodecUint32) BinarySize() uint64 { return c.size }

func (c *testCodecUint32) ValueAt(offset uint64) uint32 {
	if offset >= c.length {
		panic(errOffsetOutOfRange)
	}
	return 0
}

func (c *testCodecUint32) Decode(dst []uint32) error {
	if err := validateDecodeLength(c.length, len(dst)); err != nil {
		return err
	}
	for i := range dst {
		dst[i] = 0
	}
	return nil
}

func (c *testCodecUint32) WriteTo(io.Writer) (int64, error) {
	return 0, nil
}

func (s testStatsUint32) Source() array.Array[uint32] {
	return s.arr
}

func (s testStatsUint32) Sample(ctx planContext) array.Array[uint32] {
	if ctx.isSample {
		return s.arr
	}
	return sampleArray(s.arr)
}

type testCompressorUint32 struct {
	schemes []scheme[uint32, testStatsUint32]
}

func (c testCompressorUint32) ComputeStats(arr array.Array[uint32]) testStatsUint32 {
	return testStatsUint32{arr: arr}
}

func (c testCompressorUint32) Schemes() []scheme[uint32, testStatsUint32] {
	return c.schemes
}

func (testCompressorUint32) IsExcluded(ctx planContext, kind CodeType) bool {
	return ctx.excludesInteger(kind)
}

func (c *spyCodec[T]) Kind() CodeType     { return CodecTypeRaw }
func (c *spyCodec[T]) Length() uint64     { return uint64(len(c.values)) }
func (c *spyCodec[T]) PType() PType       { return pTypeForType[T]() }
func (c *spyCodec[T]) BinarySize() uint64 { return 0 }

func (c *spyCodec[T]) ValueAt(offset uint64) T {
	if offset >= uint64(len(c.values)) {
		panic(errOffsetOutOfRange)
	}
	return c.values[offset]
}

func (c *spyCodec[T]) Decode(dst []T) error {
	c.decodeCalls++
	if c.decodeErr != nil {
		return c.decodeErr
	}
	if err := validateDecodeLength(uint64(len(c.values)), len(dst)); err != nil {
		return err
	}
	copy(dst, c.values)
	return nil
}

func (c *spyCodec[T]) WriteTo(io.Writer) (int64, error) {
	return 0, nil
}

func equalFloats[T Float](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !cmpFloats(a[i], b[i]) {
			return false
		}
	}
	return true
}

func TestCompressIntsRoundTrip(t *testing.T) {
	values := []int32{-7, -3, -7, -3, -7, -3, -7, -3, -7, -3, -7, -3}
	codec, err := CompressInts(values, Options{})
	require.NoError(t, err)
	require.LessOrEqual(t, codec.BinarySize(), rawBinarySize(buildArray(values)))

	decoded := make([]int32, len(values))
	require.NoError(t, codec.Decode(decoded))
	require.Equal(t, values, decoded)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readCodec, err := Read[int32](&buf)
	require.NoError(t, err)
	decoded = make([]int32, len(values))
	require.NoError(t, readCodec.Decode(decoded))
	require.Equal(t, values, decoded)
}

func TestCompressUintsRoundTrip(t *testing.T) {
	values := []uint32{1000, 1002, 1004, 1006, 1008, 1010, 1012, 1014, 1016, 1018}
	codec, err := CompressUints(values, Options{})
	require.NoError(t, err)
	require.LessOrEqual(t, codec.BinarySize(), rawBinarySize(buildArray(values)))

	decoded := make([]uint32, len(values))
	require.NoError(t, codec.Decode(decoded))
	require.Equal(t, values, decoded)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readCodec, err := Read[uint32](&buf)
	require.NoError(t, err)
	decoded = make([]uint32, len(values))
	require.NoError(t, readCodec.Decode(decoded))
	require.Equal(t, values, decoded)
}

func TestCompressFloatsRoundTrip(t *testing.T) {
	values := []float64{12.34, 12.35, 12.34, 12.35, 12.34, 12.35, 12.34, 12.35}
	codec, err := CompressFloats(values, Options{})
	require.NoError(t, err)
	require.LessOrEqual(t, codec.BinarySize(), rawBinarySize(buildArray(values)))

	decoded := make([]float64, len(values))
	require.NoError(t, codec.Decode(decoded))
	require.True(t, equalFloats(values, decoded))

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readCodec, err := Read[float64](&buf)
	require.NoError(t, err)
	decoded = make([]float64, len(values))
	require.NoError(t, readCodec.Decode(decoded))
	require.True(t, equalFloats(values, decoded))
}

func TestCompressStringsRoundTrip(t *testing.T) {
	values := []string{"foo", "foo", "bar", "foo", "foo", "bar", "foo", "foo", "bar"}
	codec, err := CompressStrings(values, Options{})
	require.NoError(t, err)
	require.LessOrEqual(t, codec.BinarySize(), rawBinarySize(buildArray(values)))

	decoded := make([]string, len(values))
	require.NoError(t, codec.Decode(decoded))
	require.Equal(t, values, decoded)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readCodec, err := Read[string](&buf)
	require.NoError(t, err)
	decoded = make([]string, len(values))
	require.NoError(t, readCodec.Decode(decoded))
	require.Equal(t, values, decoded)
}

func TestDecompressFromCodec(t *testing.T) {
	values := []int32{-7, -3, -7, -3, -7, -3, -7, -3, -7, -3, -7, -3}
	codec, err := CompressInts(values, Options{})
	require.NoError(t, err)

	decompressed, err := Decompress(codec)
	require.NoError(t, err)
	require.Equal(t, values, decompressed)
}

func TestDecompressFromReadCodec(t *testing.T) {
	values := []string{"foo", "foo", "bar", "foo", "foo", "bar", "foo", "foo", "bar"}
	codec, err := CompressStrings(values, Options{})
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readCodec, err := Read[string](&buf)
	require.NoError(t, err)

	decompressed, err := Decompress(readCodec)
	require.NoError(t, err)
	require.Equal(t, values, decompressed)
}

func TestDecompressIntoCodec(t *testing.T) {
	values := []uint32{1000, 1002, 1004, 1006, 1008, 1010, 1012, 1014, 1016, 1018}
	codec, err := CompressUints(values, Options{})
	require.NoError(t, err)

	decompressed := make([]uint32, len(values))
	require.NoError(t, DecompressInto(codec, decompressed))
	require.Equal(t, values, decompressed)
}

func TestDecompressIntoRejectsLengthMismatch(t *testing.T) {
	values := []int32{-7, -3, -7, -3}
	codec, err := CompressInts(values, Options{})
	require.NoError(t, err)

	err = DecompressInto(codec, make([]int32, len(values)-1))
	require.Error(t, err)
	require.Contains(t, err.Error(), "decode destination length")
}

func TestRunEndDecodeUsesChildDecode(t *testing.T) {
	runs := &spyCodec[uint32]{
		values: []uint32{10, 20, 30},
	}
	ends := &spyCodec[uint8]{
		values: []uint8{2, 5},
	}
	codec := &runEndCodec[uint32, uint8]{
		length: 7,
		runs:   runs,
		ends:   ends,
	}

	decoded := make([]uint32, codec.Length())
	require.NoError(t, codec.Decode(decoded))
	require.Equal(t, []uint32{10, 10, 20, 20, 20, 30, 30}, decoded)
	require.Equal(t, 1, runs.decodeCalls)
	require.Equal(t, 1, ends.decodeCalls)
}

func TestRunEndCodecRoundTripAfterRead(t *testing.T) {
	values := []uint32{5, 5, 5, 7, 7, 9, 9, 9}
	codec, err := buildRunEndCodec(array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}), cmpIntegers[uint32])
	require.NoError(t, err)
	require.Equal(t, CodecTypeRunEnd, codec.Kind())

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readCodec, err := Read[uint32](&buf)
	require.NoError(t, err)

	decoded := make([]uint32, len(values))
	require.NoError(t, readCodec.Decode(decoded))
	require.Equal(t, values, decoded)
}

func TestDictDecodeUsesChildDecode(t *testing.T) {
	values := &spyCodec[uint32]{
		values: []uint32{10, 20},
	}
	indices := &spyCodec[uint8]{
		values: []uint8{1, 0, 1, 1},
	}
	codec := &dictCodec[uint32, uint8]{
		values:  values,
		indices: indices,
	}

	decoded := make([]uint32, codec.Length())
	require.NoError(t, codec.Decode(decoded))
	require.Equal(t, []uint32{20, 10, 20, 20}, decoded)
	require.Equal(t, 1, values.decodeCalls)
	require.Equal(t, 1, indices.decodeCalls)
}

func TestDictCodecRoundTripAfterRead(t *testing.T) {
	values := []uint32{7, 9, 7, 9, 7, 9}
	codec, err := buildIntegerDictCodec(array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)
	require.Equal(t, CodecTypeDict, codec.Kind())

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readCodec, err := Read[uint32](&buf)
	require.NoError(t, err)

	decoded := make([]uint32, len(values))
	require.NoError(t, readCodec.Decode(decoded))
	require.Equal(t, values, decoded)
}

func TestSequenceSelected(t *testing.T) {
	values := []uint32{1000000, 1000003, 1000006, 1000009, 1000012, 1000015}
	codec, err := CompressUints(values, Options{})
	require.NoError(t, err)
	require.Equal(t, CodecTypeSequence, codec.Kind())
}

func TestFoRUsesBitpackChild(t *testing.T) {
	codec, err := buildFoRCodec(array.NewPrimitivesUnsafe([]uint32{1000, 1002, 1004, 1006}), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	forCodec, ok := codec.(*forCodec[uint32])
	require.True(t, ok)
	require.IsType(t, &bitpackCodec[uint32]{}, forCodec.child)
}

func TestIntegerDictKeepsValuesRaw(t *testing.T) {
	codec, err := buildIntegerDictCodec(array.NewPrimitivesUnsafe([]int32{7, 9, 7, 9, 7, 9}), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	dict, ok := codec.(*dictCodec[int32, uint8])
	require.True(t, ok)
	require.IsType(t, &rawCodec[int32]{}, dict.values)
}

func TestStringDictExcludesNestedDictOnValues(t *testing.T) {
	codec, err := buildStringDictCodec(array.NewStrings([]string{"aa", "bb", "aa", "bb", "aa", "bb"}), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	dict, ok := codec.(*dictCodec[string, uint8])
	require.True(t, ok)
	require.NotEqual(t, CodecTypeDict, dict.values.Kind())
}

func requireSupportedKind(t *testing.T, kind CodeType) {
	t.Helper()

	switch kind {
	case CodecTypeConst, CodecTypeRaw, CodecTypeDict, CodecTypeRunEnd, CodecTypeZigZag, CodecTypeBitpack, CodecTypeFor, CodecTypeSequence, CodecTypeALP:
	default:
		t.Fatalf("unsupported codec kind %d", kind)
	}
}

func TestCompressIntsUsesSupportedCodec(t *testing.T) {
	codec, err := CompressInts([]int32{0, 0, 0, 5, 0, 0, 0}, Options{})
	require.NoError(t, err)
	requireSupportedKind(t, codec.Kind())
}

func TestCompressUintsUsesSupportedCodec(t *testing.T) {
	codec, err := CompressUints([]uint32{0, 0, 0, 5, 0, 0, 0}, Options{})
	require.NoError(t, err)
	requireSupportedKind(t, codec.Kind())
}

func TestCompressFloatsUsesSupportedCodec(t *testing.T) {
	codec, err := CompressFloats([]float64{0, 0, 0, 5.5, 0, 0, 0}, Options{})
	require.NoError(t, err)
	requireSupportedKind(t, codec.Kind())
}

func TestCompressStringsUsesSupportedCodec(t *testing.T) {
	codec, err := CompressStrings([]string{"", "", "", "x", "", "", ""}, Options{})
	require.NoError(t, err)
	requireSupportedKind(t, codec.Kind())
}

func TestChooseSchemeBuildsOnlyWinner(t *testing.T) {
	arr := array.NewPrimitivesUnsafe([]uint32{1, 2, 3, 4})
	rawSize := newRawCodec[uint32](arr).BinarySize()
	firstBuilds := 0
	secondBuilds := 0
	compressor := testCompressorUint32{
		schemes: []scheme[uint32, testStatsUint32]{
			registeredScheme[uint32, testStatsUint32]{
				kind: CodecTypeBitpack,
				estimate: func(testStatsUint32, planContext) (float64, bool) {
					return 10, true
				},
				build: func(array.Array[uint32], planContext) (Codec[uint32], error) {
					firstBuilds++
					return &testCodecUint32{kind: CodecTypeBitpack, length: arr.Length(), size: rawSize - 4}, nil
				},
			},
			registeredScheme[uint32, testStatsUint32]{
				kind: CodecTypeFor,
				estimate: func(testStatsUint32, planContext) (float64, bool) {
					return 2, true
				},
				build: func(array.Array[uint32], planContext) (Codec[uint32], error) {
					secondBuilds++
					return &testCodecUint32{kind: CodecTypeFor, length: arr.Length(), size: rawSize - 12}, nil
				},
			},
		},
	}
	codec, err := compressWith(arr, newPlanContext(Options{}), compressor)
	require.NoError(t, err)
	require.Equal(t, 1, firstBuilds)
	require.Equal(t, 0, secondBuilds)
	require.Equal(t, CodecTypeBitpack, codec.Kind())
	require.Equal(t, rawSize-4, codec.BinarySize())
}

func TestCompressWithKeepsRawWhenWinnerDoesNotBeatIt(t *testing.T) {
	arr := array.NewPrimitivesUnsafe([]uint32{1, 2, 3, 4})
	rawSize := newRawCodec[uint32](arr).BinarySize()
	compressor := testCompressorUint32{
		schemes: []scheme[uint32, testStatsUint32]{
			registeredScheme[uint32, testStatsUint32]{
				kind: CodecTypeBitpack,
				estimate: func(testStatsUint32, planContext) (float64, bool) {
					return 2, true
				},
				build: func(array.Array[uint32], planContext) (Codec[uint32], error) {
					return &testCodecUint32{kind: CodecTypeBitpack, length: arr.Length(), size: rawSize}, nil
				},
			},
			registeredScheme[uint32, testStatsUint32]{
				kind: CodecTypeFor,
				estimate: func(testStatsUint32, planContext) (float64, bool) {
					return 3, true
				},
				build: func(array.Array[uint32], planContext) (Codec[uint32], error) {
					return &testCodecUint32{kind: CodecTypeFor, length: arr.Length(), size: rawSize + 8}, nil
				},
			},
		},
	}
	codec, err := compressWith(arr, newPlanContext(Options{}), compressor)
	require.NoError(t, err)
	require.Equal(t, CodecTypeRaw, codec.Kind())
	require.Equal(t, rawSize, codec.BinarySize())
}

func TestCompressCanonicalNullableIntsRoundTrip(t *testing.T) {
	codec, err := CompressCanonical([]int32{7, 0, 9, 0, 7}, []bool{true, false, true, false, true}, Options{})
	require.NoError(t, err)
	require.Equal(t, uint64(2), codec.NullCount())

	values, valid, err := DecompressCanonical(codec)
	require.NoError(t, err)
	require.Equal(t, []bool{true, false, true, false, true}, valid)
	require.Equal(t, []int32{7, 0, 9, 0, 7}, values)
}

func TestCompressCanonicalNullableStringsRoundTrip(t *testing.T) {
	codec, err := CompressCanonical([]string{"aa", "", "bb", ""}, []bool{true, false, true, false}, Options{})
	require.NoError(t, err)
	require.Equal(t, uint64(2), codec.NullCount())

	values, valid, err := DecompressCanonical(codec)
	require.NoError(t, err)
	require.Equal(t, []bool{true, false, true, false}, valid)
	require.Equal(t, []string{"aa", "", "bb", ""}, values)
}

func TestCompressCanonicalAllNullsRoundTrip(t *testing.T) {
	codec, err := CompressCanonical[int64](nil, []bool{false, false, false}, Options{})
	require.NoError(t, err)
	require.Equal(t, uint64(3), codec.NullCount())

	values, valid, err := DecompressCanonical(codec)
	require.NoError(t, err)
	require.Equal(t, []bool{false, false, false}, valid)
	require.Equal(t, []int64{0, 0, 0}, values)
}

func TestUnsignedOffsetRangeChoosesFoRWhenBitpackIsExcludedByCost(t *testing.T) {
	values := make([]uint32, 512)
	pattern := []uint32{1000, 1007, 1001, 1006, 1002, 1005, 1003, 1004}
	for i := range values {
		values[i] = pattern[i%len(pattern)]
	}

	codec, err := compressWith(
		array.NewPrimitivesUnsafe(values),
		newPlanContext(Options{MaxDepth: 3}).withIntegerExcludes(CodecTypeDict, CodecTypeSequence, CodecTypeRunEnd),
		unsignedIntCompressor[uint32]{},
	)
	require.NoError(t, err)
	require.Equal(t, CodecTypeFor, codec.Kind())
}

func TestUnsignedSmallOffsetRangeKeepsBitpackWhenFoROverheadIsTooHigh(t *testing.T) {
	values := []uint32{1000, 1007, 1001, 1006}

	codec, err := compressWith(
		array.NewPrimitivesUnsafe(values),
		newPlanContext(Options{MaxDepth: 3}).withIntegerExcludes(CodecTypeDict, CodecTypeSequence, CodecTypeRunEnd),
		unsignedIntCompressor[uint32]{},
	)
	require.NoError(t, err)
	require.Equal(t, CodecTypeBitpack, codec.Kind())
}

func TestUnsignedLowCardinalityLargeValuesChooseDict(t *testing.T) {
	values := make([]uint32, 512)
	pattern := []uint32{1_000_000, 2_000_000, 3_000_000}
	for i := range values {
		values[i] = pattern[i%len(pattern)]
	}

	codec, err := compressWith(
		array.NewPrimitivesUnsafe(values),
		newPlanContext(Options{MaxDepth: 3}).withIntegerExcludes(CodecTypeSequence, CodecTypeRunEnd),
		unsignedIntCompressor[uint32]{},
	)
	require.NoError(t, err)
	require.Equal(t, CodecTypeDict, codec.Kind())
}

func TestUnsignedOutliersChoosePatchedBitpack(t *testing.T) {
	values := make([]uint32, 1024)
	for i := range values {
		values[i] = uint32(i % 16)
	}
	values[100] = 1 << 20
	values[400] = 1<<20 + 7
	values[900] = 1 << 19

	codec, err := compressWith(
		array.NewPrimitivesUnsafe(values),
		newPlanContext(Options{MaxDepth: 3}).withIntegerExcludes(CodecTypeDict, CodecTypeSequence, CodecTypeRunEnd, CodecTypeFor),
		unsignedIntCompressor[uint32]{},
	)
	require.NoError(t, err)
	require.Equal(t, CodecTypeBitpack, codec.Kind())

	bitpack, ok := codec.(*bitpackCodec[uint32])
	require.True(t, ok)
	require.NotNil(t, bitpack.patchIdxC)
	require.NotNil(t, bitpack.patchValC)
	require.Less(t, bitpack.bitWidth, bitWidthForUnsigned(uint64(values[400])))
}

func TestALPPropagatesFloatDictExcludesToIntegerChild(t *testing.T) {
	values := make([]float64, 256)
	pattern := []float64{12.34, 56.78}
	for i := range values {
		values[i] = pattern[i%len(pattern)]
	}

	codec, err := buildALPCodec(
		array.NewPrimitivesUnsafe(values),
		newPlanContext(Options{MaxDepth: 3}).withFloatExcludes(CodecTypeDict),
	)
	require.NoError(t, err)

	alp, ok := codec.(*alpCodec64)
	require.True(t, ok)
	require.NotEqual(t, CodecTypeDict, alp.encoded.Kind())
}
