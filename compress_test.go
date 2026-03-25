package btrblocks

import (
	"bytes"
	"io"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

// testEncodedArrayUint32 is a minimal encoded-array stub used by planner selection tests.
type testEncodedArrayUint32 struct {
	kind   CodeType
	length uint64
	size   uint64
}

// testStatsUint32 is a lightweight statsSource implementation for planner tests.
type testStatsUint32 struct {
	arr array.Array[uint32]
}

// spyArray is a simple in-memory encoded array used by tests.
type spyArray[T Integer | Float | String] struct {
	values []T
}

func (c *testEncodedArrayUint32) Encoding() CodeType { return c.kind }
func (c *testEncodedArrayUint32) Length() uint64     { return c.length }
func (c *testEncodedArrayUint32) PType() PType       { return PTypeUint32 }
func (c *testEncodedArrayUint32) BinarySize() uint64 { return c.size }

func (c *testEncodedArrayUint32) ValueAt(offset uint64) uint32 {
	if offset >= c.length {
		panic(errOffsetOutOfRange)
	}
	return 0
}

func (c *testEncodedArrayUint32) DecompressInto(dst []uint32) error {
	for i := uint64(0); i < c.length; i++ {
		dst[i] = c.ValueAt(i)
	}
	return nil
}

func (c *testEncodedArrayUint32) Decompress() ([]uint32, error) {
	dst := make([]uint32, c.length)
	return dst, c.DecompressInto(dst)
}

func (c *testEncodedArrayUint32) Slice(start, end uint64) (EncodedArray[uint32], error) {
	if err := array.ValidateSliceBounds(c.length, start, end); err != nil {
		return nil, err
	}
	return &testEncodedArrayUint32{kind: c.kind, length: end - start, size: c.size}, nil
}

func (c *testEncodedArrayUint32) WriteTo(io.Writer) (int64, error) {
	return 0, nil
}

func (s testStatsUint32) Source() array.Array[uint32] {
	return s.arr
}

func (s testStatsUint32) Sample(ctx planContext) array.ArrayCore[uint32] {
	if ctx.isSample {
		return s.arr
	}
	return sampleArray(s.arr)
}

// testCompressorUint32 exposes injected schemes to exercise planner behavior.
type testCompressorUint32 struct {
	schemes []scheme[uint32, testStatsUint32]
}

func (c testCompressorUint32) ComputeStats(arr array.Array[uint32]) testStatsUint32 {
	return testStatsUint32{arr: arr}
}

func (testCompressorUint32) DefaultScheme() scheme[uint32, testStatsUint32] {
	return rawScheme[uint32, testStatsUint32]()
}

func (c testCompressorUint32) Schemes(_ testStatsUint32) []scheme[uint32, testStatsUint32] {
	return c.schemes
}

func (testCompressorUint32) IsExcluded(ctx planContext, kind CodeType) bool {
	return ctx.excludesInteger(kind)
}

func (c *spyArray[T]) Encoding() CodeType { return CodecTypeRaw }
func (c *spyArray[T]) Length() uint64     { return uint64(len(c.values)) }
func (c *spyArray[T]) PType() PType       { return array.PTypeForType[T]() }
func (c *spyArray[T]) BinarySize() uint64 { return 0 }

func (c *spyArray[T]) ValueAt(offset uint64) T {
	if offset >= uint64(len(c.values)) {
		panic(errOffsetOutOfRange)
	}
	return c.values[offset]
}

func (c *spyArray[T]) DecompressInto(dst []T) error {
	copy(dst, c.values)
	return nil
}

func (c *spyArray[T]) Decompress() ([]T, error) {
	dst := make([]T, len(c.values))
	return dst, c.DecompressInto(dst)
}

func (c *spyArray[T]) Slice(start, end uint64) (EncodedArray[T], error) {
	if err := array.ValidateSliceBounds(uint64(len(c.values)), start, end); err != nil {
		return nil, err
	}
	values := append([]T(nil), c.values[int(start):int(end)]...)
	return &spyArray[T]{values: values}, nil
}

func (c *spyArray[T]) WriteTo(io.Writer) (int64, error) {
	return 0, nil
}

func equalFloats[T Float](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !cmpFloatBits(a[i], b[i]) {
			return false
		}
	}
	return true
}

func TestCompressRoundTripInts(t *testing.T) {
	values := []int32{-7, -3, -7, -3, -7, -3, -7, -3, -7, -3, -7, -3}
	codec, err := Compress(buildArray(values), Options{})
	require.NoError(t, err)
	require.LessOrEqual(t, codec.BinarySize(), newRawArray(buildArray(values)).BinarySize())

	decoded, err := Decompress(codec)
	require.NoError(t, err)
	require.Equal(t, values, decoded)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readEncodedArray, err := Load[int32](buf.Bytes())
	require.NoError(t, err)
	decoded, err = Decompress(readEncodedArray)
	require.NoError(t, err)
	require.Equal(t, values, decoded)
}

func TestCompressRoundTripUints(t *testing.T) {
	values := []uint32{1000, 1002, 1004, 1006, 1008, 1010, 1012, 1014, 1016, 1018}
	codec, err := Compress(buildArray(values), Options{})
	require.NoError(t, err)
	require.LessOrEqual(t, codec.BinarySize(), newRawArray(buildArray(values)).BinarySize())

	decoded, err := Decompress(codec)
	require.NoError(t, err)
	require.Equal(t, values, decoded)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readEncodedArray, err := Load[uint32](buf.Bytes())
	require.NoError(t, err)
	decoded, err = Decompress(readEncodedArray)
	require.NoError(t, err)
	require.Equal(t, values, decoded)
}

func TestCompressRoundTripFloats(t *testing.T) {
	values := []float64{12.34, 12.35, 12.34, 12.35, 12.34, 12.35, 12.34, 12.35}
	codec, err := Compress(buildArray(values), Options{})
	require.NoError(t, err)
	require.LessOrEqual(t, codec.BinarySize(), newRawArray(buildArray(values)).BinarySize())

	decoded, err := Decompress(codec)
	require.NoError(t, err)
	require.True(t, equalFloats(values, decoded))

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readEncodedArray, err := Load[float64](buf.Bytes())
	require.NoError(t, err)
	decoded, err = Decompress(readEncodedArray)
	require.NoError(t, err)
	require.True(t, equalFloats(values, decoded))
}

func TestCompressRoundTripStrings(t *testing.T) {
	values := []string{"foo", "foo", "bar", "foo", "foo", "bar", "foo", "foo", "bar"}
	codec, err := Compress(buildArray(values), Options{})
	require.NoError(t, err)
	require.LessOrEqual(t, codec.BinarySize(), newRawArray(buildArray(values)).BinarySize())

	decoded, err := Decompress(codec)
	require.NoError(t, err)
	require.Equal(t, values, decoded)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readEncodedArray, err := Load[string](buf.Bytes())
	require.NoError(t, err)
	decoded, err = Decompress(readEncodedArray)
	require.NoError(t, err)
	require.Equal(t, values, decoded)
}

func TestCompressConstantStringsUsesConst(t *testing.T) {
	values := []string{"constant", "constant", "constant", "constant"}

	codec, err := Compress(buildArray(values), Options{})
	require.NoError(t, err)
	require.Equal(t, CodecTypeConst, codec.Encoding())
}

func TestDecompressFromCodec(t *testing.T) {
	values := []int32{-7, -3, -7, -3, -7, -3, -7, -3, -7, -3, -7, -3}
	codec, err := Compress(buildArray(values), Options{})
	require.NoError(t, err)

	decompressed, err := Decompress(codec)
	require.NoError(t, err)
	require.Equal(t, values, decompressed)
}

func TestDecompressFromReadCodec(t *testing.T) {
	values := []string{"foo", "foo", "bar", "foo", "foo", "bar", "foo", "foo", "bar"}
	codec, err := Compress(buildArray(values), Options{})
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readEncodedArray, err := Load[string](buf.Bytes())
	require.NoError(t, err)

	decompressed, err := Decompress(readEncodedArray)
	require.NoError(t, err)
	require.Equal(t, values, decompressed)
}

func TestDecompressCodec(t *testing.T) {
	values := []uint32{1000, 1002, 1004, 1006, 1008, 1010, 1012, 1014, 1016, 1018}
	codec, err := Compress(buildArray(values), Options{})
	require.NoError(t, err)

	decompressed, err := Decompress(codec)
	require.NoError(t, err)
	require.Equal(t, values, decompressed)
}

func TestRunEndDecompressUsesValueAt(t *testing.T) {
	runs := &spyArray[uint32]{
		values: []uint32{10, 20, 30},
	}
	ends := &spyArray[uint8]{
		values: []uint8{2, 5},
	}
	codec := &runEndArray[uint32, uint8]{
		length: 7,
		runs:   runs,
		ends:   ends,
	}

	decoded, err := Decompress(codec)
	require.NoError(t, err)
	require.Equal(t, []uint32{10, 10, 20, 20, 20, 30, 30}, decoded)
}

func TestRunEndCodecRoundTripAfterRead(t *testing.T) {
	values := []uint32{5, 5, 5, 7, 7, 9, 9, 9}
	codec, err := buildRunEndArray(array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}), cmpIntegers[uint32])
	require.NoError(t, err)
	require.Equal(t, CodecTypeRunEnd, codec.Encoding())

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readEncodedArray, err := Load[uint32](buf.Bytes())
	require.NoError(t, err)

	decoded, err := Decompress(readEncodedArray)
	require.NoError(t, err)
	require.Equal(t, values, decoded)
}

func TestDictDecompressUsesValueAt(t *testing.T) {
	values := &spyArray[uint32]{
		values: []uint32{10, 20},
	}
	indices := &spyArray[uint8]{
		values: []uint8{1, 0, 1, 1},
	}
	codec := &dictArray[uint32, uint8]{
		values:  values,
		indices: indices,
	}

	decoded, err := Decompress(codec)
	require.NoError(t, err)
	require.Equal(t, []uint32{20, 10, 20, 20}, decoded)
}

func TestDictCodecRoundTripAfterRead(t *testing.T) {
	values := []uint32{7, 9, 7, 9, 7, 9}
	codec, err := buildIntegerDictFromDistinct(array.NewPrimitivesUnsafe(values), nil, newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)
	require.Equal(t, CodecTypeDict, codec.Encoding())

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readEncodedArray, err := Load[uint32](buf.Bytes())
	require.NoError(t, err)

	decoded, err := Decompress(readEncodedArray)
	require.NoError(t, err)
	require.Equal(t, values, decoded)
}

func TestSequenceSelected(t *testing.T) {
	// Use 100+ elements so sequence's byte-size ratio clearly outranks FoR's
	// bit-width ratio. With Vortex-style analytical estimates, FoR may win on
	// tiny arrays because bit-width ratios ignore per-node header overhead.
	values := make([]uint32, 100)
	for i := range values {
		values[i] = 1000000 + uint32(i)*3
	}
	codec, err := Compress(buildArray(values), Options{})
	require.NoError(t, err)
	require.Equal(t, CodecTypeSequence, codec.Encoding())
}

func TestFoRUsesBitpackChild(t *testing.T) {
	codec, err := buildFoRArray(array.NewPrimitivesUnsafe([]uint32{1000, 1002, 1004, 1006}), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	forArray, ok := codec.(*forArray[uint32])
	require.True(t, ok)
	require.IsType(t, &bitPackedArray[uint32, uint64]{}, forArray.child)
}

func TestIntegerDictCompressesValues(t *testing.T) {
	codec, err := buildIntegerDictFromDistinct(array.NewPrimitivesUnsafe([]int32{7, 9, 7, 9, 7, 9}), nil, newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	dict, ok := codec.(*dictArray[int32, uint8])
	require.True(t, ok)
	// Values are recursively compressed (excluding Dict to prevent loops).
	require.NotEqual(t, CodecTypeDict, dict.values.Encoding())
}

func TestStringDictExcludesNestedDictOnValues(t *testing.T) {
	codec, err := buildStringDictArray(array.NewStrings([]string{"aa", "bb", "aa", "bb", "aa", "bb"}), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	dict, ok := codec.(*dictArray[string, uint8])
	require.True(t, ok)
	require.NotEqual(t, CodecTypeDict, dict.values.Encoding())
}

func requireSupportedEncoding(t *testing.T, kind CodeType) {
	t.Helper()

	switch kind {
	case CodecTypeConst, CodecTypeRaw, CodecTypeDict, CodecTypeRunEnd, CodecTypeZigZag, CodecTypeBitpack, CodecTypeFor, CodecTypeSequence, CodecTypeALP, CodecTypeALPRD:
	default:
		t.Fatalf("unsupported codec kind %d", kind)
	}
}

func TestCompressUsesSupportedCodecInts(t *testing.T) {
	codec, err := Compress(buildArray([]int32{0, 0, 0, 5, 0, 0, 0}), Options{})
	require.NoError(t, err)
	requireSupportedEncoding(t, codec.Encoding())
}

func TestCompressUsesSupportedCodecUints(t *testing.T) {
	codec, err := Compress(buildArray([]uint32{0, 0, 0, 5, 0, 0, 0}), Options{})
	require.NoError(t, err)
	requireSupportedEncoding(t, codec.Encoding())
}

func TestCompressUsesSupportedCodecFloats(t *testing.T) {
	codec, err := Compress(buildArray([]float64{0, 0, 0, 5.5, 0, 0, 0}), Options{})
	require.NoError(t, err)
	requireSupportedEncoding(t, codec.Encoding())
}

func TestCompressUsesSupportedCodecStrings(t *testing.T) {
	codec, err := Compress(buildArray([]string{"", "", "", "x", "", "", ""}), Options{})
	require.NoError(t, err)
	requireSupportedEncoding(t, codec.Encoding())
}

func TestChooseSchemeBuildsOnlyWinner(t *testing.T) {
	arr := array.NewPrimitivesUnsafe([]uint32{1, 2, 3, 4})
	rawSize := newRawArray(arr).BinarySize()
	firstBuilds := 0
	secondBuilds := 0
	compressor := testCompressorUint32{
		schemes: []scheme[uint32, testStatsUint32]{
			registeredScheme[uint32, testStatsUint32]{
				kind: CodecTypeBitpack,
				estimate: func(testStatsUint32, planContext) (float64, bool) {
					return 10, true
				},
				build: func(array.ArrayCore[uint32], planContext) (EncodedArray[uint32], error) {
					firstBuilds++
					return &testEncodedArrayUint32{kind: CodecTypeBitpack, length: arr.Length(), size: rawSize - 4}, nil
				},
			},
			registeredScheme[uint32, testStatsUint32]{
				kind: CodecTypeFor,
				estimate: func(testStatsUint32, planContext) (float64, bool) {
					return 2, true
				},
				build: func(array.ArrayCore[uint32], planContext) (EncodedArray[uint32], error) {
					secondBuilds++
					return &testEncodedArrayUint32{kind: CodecTypeFor, length: arr.Length(), size: rawSize - 12}, nil
				},
			},
		},
	}
	codec, err := compressWith(arr, newPlanContext(Options{}), compressor)
	require.NoError(t, err)
	require.Equal(t, 1, firstBuilds)
	require.Equal(t, 0, secondBuilds)
	require.Equal(t, CodecTypeBitpack, codec.Encoding())
	require.Equal(t, rawSize-4, codec.BinarySize())
}

func TestCompressWithKeepsRawWhenWinnerDoesNotBeatIt(t *testing.T) {
	arr := array.NewPrimitivesUnsafe([]uint32{1, 2, 3, 4})
	rawSize := newRawArray(arr).BinarySize()
	compressor := testCompressorUint32{
		schemes: []scheme[uint32, testStatsUint32]{
			registeredScheme[uint32, testStatsUint32]{
				kind: CodecTypeBitpack,
				estimate: func(testStatsUint32, planContext) (float64, bool) {
					return 2, true
				},
				build: func(array.ArrayCore[uint32], planContext) (EncodedArray[uint32], error) {
					return &testEncodedArrayUint32{kind: CodecTypeBitpack, length: arr.Length(), size: rawSize}, nil
				},
			},
			registeredScheme[uint32, testStatsUint32]{
				kind: CodecTypeFor,
				estimate: func(testStatsUint32, planContext) (float64, bool) {
					return 3, true
				},
				build: func(array.ArrayCore[uint32], planContext) (EncodedArray[uint32], error) {
					return &testEncodedArrayUint32{kind: CodecTypeFor, length: arr.Length(), size: rawSize + 8}, nil
				},
			},
		},
	}
	codec, err := compressWith(arr, newPlanContext(Options{}), compressor)
	require.NoError(t, err)
	require.Equal(t, CodecTypeRaw, codec.Encoding())
	require.Equal(t, rawSize, codec.BinarySize())
}

func TestCompressWithKeepsBorrowedRawWhenKeepingRaw(t *testing.T) {
	values := []uint32{1, 2, 3, 4}
	codec, err := compressWith(array.NewPrimitivesUnsafe(values), newPlanContext(Options{}), testCompressorUint32{})
	require.NoError(t, err)
	require.Equal(t, CodecTypeRaw, codec.Encoding())

	values[0] = 99
	values[1] = 88

	decoded, err := Decompress(codec)
	require.NoError(t, err)
	require.Equal(t, []uint32{99, 88, 3, 4}, decoded)
}

func TestCompressWithKeepsBorrowedRawWhenBuildFails(t *testing.T) {
	t.Run("expected error falls back to raw", func(t *testing.T) {
		values := []uint32{10, 20, 30, 40}
		compressor := testCompressorUint32{
			schemes: []scheme[uint32, testStatsUint32]{
				registeredScheme[uint32, testStatsUint32]{
					kind: CodecTypeBitpack,
					estimate: func(testStatsUint32, planContext) (float64, bool) {
						return 2, true
					},
					build: func(array.ArrayCore[uint32], planContext) (EncodedArray[uint32], error) {
						return nil, errDepthExhausted
					},
				},
			},
		}

		codec, err := compressWith(array.NewPrimitivesUnsafe(values), newPlanContext(Options{}), compressor)
		require.NoError(t, err)
		require.Equal(t, CodecTypeRaw, codec.Encoding())

		values[2] = 777

		decoded, err := Decompress(codec)
		require.NoError(t, err)
		require.Equal(t, []uint32{10, 20, 777, 40}, decoded)
	})

	t.Run("unexpected error propagates", func(t *testing.T) {
		values := []uint32{10, 20, 30, 40}
		compressor := testCompressorUint32{
			schemes: []scheme[uint32, testStatsUint32]{
				registeredScheme[uint32, testStatsUint32]{
					kind: CodecTypeBitpack,
					estimate: func(testStatsUint32, planContext) (float64, bool) {
						return 2, true
					},
					build: func(array.ArrayCore[uint32], planContext) (EncodedArray[uint32], error) {
						return nil, io.ErrUnexpectedEOF
					},
				},
			},
		}

		_, err := compressWith(array.NewPrimitivesUnsafe(values), newPlanContext(Options{}), compressor)
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	})
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
		&unsignedIntCompressor[uint32]{},
	)
	require.NoError(t, err)
	require.Equal(t, CodecTypeFor, codec.Encoding())
}

func TestUnsignedSmallOffsetRangeChoosesFoRWithBitWidthEstimate(t *testing.T) {
	// With Vortex-style bit-width ratio estimation, FoR always wins over
	// direct bitpack when the range is narrower — per-node header overhead
	// is deliberately not modeled. This matches Vortex's FORScheme which has
	// no minimum array-size guard. For production-scale arrays the overhead
	// is noise; for tiny arrays the absolute waste is negligible.
	values := []uint32{1000, 1007, 1001, 1006}

	codec, err := compressWith(
		array.NewPrimitivesUnsafe(values),
		newPlanContext(Options{MaxDepth: 3}).withIntegerExcludes(CodecTypeDict, CodecTypeSequence, CodecTypeRunEnd),
		&unsignedIntCompressor[uint32]{},
	)
	require.NoError(t, err)
	require.Equal(t, CodecTypeFor, codec.Encoding())

	decoded, err := Decompress(codec)
	require.NoError(t, err)
	require.Equal(t, values, decoded)
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
		&unsignedIntCompressor[uint32]{},
	)
	require.NoError(t, err)
	require.Equal(t, CodecTypeDict, codec.Encoding())
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
		&unsignedIntCompressor[uint32]{},
	)
	require.NoError(t, err)
	require.Equal(t, CodecTypeBitpack, codec.Encoding())

	bitpack, ok := codec.(*bitPackedArray[uint32, uint64])
	require.True(t, ok)
	require.NotNil(t, bitpack.patches)
	require.Less(t, bitpack.bitWidth, bitWidthForUnsigned(uint64(values[400])))
}

func TestALPPropagatesFloatDictExcludesToIntegerChild(t *testing.T) {
	values := make([]float64, 256)
	pattern := []float64{12.34, 56.78}
	for i := range values {
		values[i] = pattern[i%len(pattern)]
	}

	codec, err := buildALPArray(
		array.NewPrimitivesUnsafe(values),
		newPlanContext(Options{MaxDepth: 3}).withFloatExcludes(CodecTypeDict),
	)
	require.NoError(t, err)

	alp, ok := codec.(*alpArray[float64, int64, uint64])
	require.True(t, ok)
	require.NotEqual(t, CodecTypeDict, alp.encoded.Encoding())
}

func TestDictSlicePreservesEncoding(t *testing.T) {
	values := []uint32{9, 7, 9, 8, 7, 8, 9}

	encoded, err := buildIntegerDictFromDistinct(array.NewPrimitivesUnsafe(values), nil, newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)

	sliced, err := encoded.Slice(1, 6)
	require.NoError(t, err)
	require.Equal(t, CodecTypeDict, sliced.Encoding())

	decoded, err := Decompress(sliced)
	require.NoError(t, err)
	require.Equal(t, values[1:6], decoded)
}

func TestBitpackSlicePreservesPatchOffsetAcrossReadWrite(t *testing.T) {
	values := make([]uint32, 256)
	for i := range values {
		values[i] = uint32(i % 8)
	}
	values[17] = 1 << 20
	values[33] = 1<<21 + 5

	encoded, err := buildBitPackedArray(array.NewPrimitivesUnsafe(values), newPlanContext(Options{}))
	require.NoError(t, err)

	sliced, err := encoded.Slice(16, 40)
	require.NoError(t, err)

	bitpack, ok := sliced.(*bitPackedArray[uint32, uint64])
	require.True(t, ok)
	require.NotNil(t, bitpack.patches)
	require.Equal(t, uint64(16), bitpack.patches.offset)

	decoded, err := Decompress(sliced)
	require.NoError(t, err)
	require.Equal(t, values[16:40], decoded)

	var buf bytes.Buffer
	_, err = sliced.WriteTo(&buf)
	require.NoError(t, err)

	readBack, err := Load[uint32](buf.Bytes())
	require.NoError(t, err)
	// After read, the patch index type may differ (narrowed on read).
	// Try uint64 first (build path), then uint8 (read path).
	if roundTrip, ok := readBack.(*bitPackedArray[uint32, uint64]); ok {
		require.NotNil(t, roundTrip.patches)
		require.Equal(t, uint64(16), roundTrip.patches.offset)
	} else if roundTrip, ok := readBack.(*bitPackedArray[uint32, uint8]); ok {
		require.NotNil(t, roundTrip.patches)
		require.Equal(t, uint64(16), roundTrip.patches.offset)
	} else if roundTrip, ok := readBack.(*bitPackedArray[uint32, uint16]); ok {
		require.NotNil(t, roundTrip.patches)
		require.Equal(t, uint64(16), roundTrip.patches.offset)
	} else {
		require.Fail(t, "unexpected bitpack type")
	}

	decoded, err = Decompress(readBack)
	require.NoError(t, err)
	require.Equal(t, values[16:40], decoded)
}

func TestALPSlicePreservesPatchOffsetAcrossReadWrite(t *testing.T) {
	encoded := &alpArray[float64, int64, uint8]{
		length:  5,
		expE:    0,
		expF:    0,
		encoded: newRawArray(buildArray([]int64{1, 2, 3, 4, 5})),
		decode:  alpDecode64,
		patches: &patches[float64, uint8]{
			length:  5,
			offset:  0,
			indices: newRawArray(array.NewPrimitivesUnsafe([]uint8{1, 4})),
			values:  newRawArray(buildArray([]float64{20.5, 50.5})),
		},
	}

	sliced, err := encoded.Slice(1, 5)
	require.NoError(t, err)

	alp := sliced.(*alpArray[float64, int64, uint8])
	require.NotNil(t, alp.patches)
	require.Equal(t, uint64(1), alp.patches.offset)

	decoded, err := Decompress(sliced)
	require.NoError(t, err)
	require.Equal(t, []float64{20.5, 3.0, 4.0, 50.5}, decoded)

	var buf bytes.Buffer
	_, err = sliced.WriteTo(&buf)
	require.NoError(t, err)

	readBack, err := Load[float64](buf.Bytes())
	require.NoError(t, err)
	roundTrip := readBack.(*alpArray[float64, int64, uint8])
	require.NotNil(t, roundTrip.patches)
	require.Equal(t, uint64(1), roundTrip.patches.offset)

	decoded, err = Decompress(readBack)
	require.NoError(t, err)
	require.Equal(t, []float64{20.5, 3.0, 4.0, 50.5}, decoded)
}

func TestExcludeIntegerPreventsDict(t *testing.T) {
	values := make([]uint32, 512)
	pattern := []uint32{100, 200, 300}
	for i := range values {
		values[i] = pattern[i%len(pattern)]
	}

	// Without exclude: dict is chosen for low-cardinality data.
	codec, err := Compress(buildArray(values), Options{})
	require.NoError(t, err)
	require.Equal(t, CodecTypeDict, codec.Encoding())

	// With exclude: dict is blocked, something else is chosen.
	codec, err = Compress(buildArray(values), Options{}.WithExcludeInteger(CodecTypeDict))
	require.NoError(t, err)
	require.NotEqual(t, CodecTypeDict, codec.Encoding())

	decoded, err := Decompress(codec)
	require.NoError(t, err)
	require.Equal(t, values, decoded)
}

func TestExcludeFloatPreventsALP(t *testing.T) {
	values := make([]float64, 256)
	for i := range values {
		values[i] = float64(i%50) * 0.01
	}

	codec, err := Compress(buildArray(values), Options{}.WithExcludeFloat(CodecTypeALP, CodecTypeALPRD))
	require.NoError(t, err)
	require.NotEqual(t, CodecTypeALP, codec.Encoding())
	require.NotEqual(t, CodecTypeALPRD, codec.Encoding())

	decoded, err := Decompress(codec)
	require.NoError(t, err)
	require.True(t, equalFloats(values, decoded))
}

func TestEmptySchemesWithIncludeRestrictsSchemes(t *testing.T) {
	// Arithmetic sequence: would normally select Sequence codec.
	values := make([]uint32, 100)
	for i := range values {
		values[i] = 1000 + uint32(i)*3
	}

	codec, err := Compress(buildArray(values), Options{})
	require.NoError(t, err)
	require.Equal(t, CodecTypeSequence, codec.Encoding())

	// Start from empty, only enable bitpack+for: sequence is excluded.
	codec, err = Compress(buildArray(values), EmptySchemes().WithIncludeInteger(CodecTypeBitpack, CodecTypeFor))
	require.NoError(t, err)
	require.NotEqual(t, CodecTypeSequence, codec.Encoding())

	decoded, err := Decompress(codec)
	require.NoError(t, err)
	require.Equal(t, values, decoded)
}

func TestIncludeAfterExcludeReEnables(t *testing.T) {
	values := make([]uint32, 100)
	for i := range values {
		values[i] = 1000 + uint32(i)*3
	}

	// Exclude sequence, then include it back — should still select sequence.
	codec, err := Compress(buildArray(values),
		Options{}.WithExcludeInteger(CodecTypeSequence).WithIncludeInteger(CodecTypeSequence))
	require.NoError(t, err)
	require.Equal(t, CodecTypeSequence, codec.Encoding())
}

func TestReadWithMaxLengthRejectsOversized(t *testing.T) {
	values := make([]uint32, 1000)
	for i := range values {
		values[i] = uint32(i % 50)
	}
	codec, err := Compress(array.NewPrimitivesUnsafe(values), Options{})
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	// Without limits: succeeds.
	_, err = Load[uint32](buf.Bytes())
	require.NoError(t, err)

	// With tight MaxLength: rejected.
	_, err = Load[uint32](buf.Bytes(), ReadOptions{MaxLength: 100})
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds limit")
}

func benchDecompress[T Integer | Float | String](b *testing.B, encoded EncodedArray[T]) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Decompress(encoded); err != nil {
			b.Fatal(err)
		}
	}
}

func benchDecompressInto[T Integer | Float | String](b *testing.B, encoded EncodedArray[T]) {
	dst := make([]T, encoded.Length())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := encoded.DecompressInto(dst); err != nil {
			b.Fatal(err)
		}
	}
}

// serializeForRead compresses values and serializes to []byte for ReadBytes benchmarks.
func serializeForRead[T Integer | Float | String](values []T, opts Options) []byte {
	codec, err := Compress(buildArray(values), opts)
	if err != nil {
		panic(err)
	}
	var buf bytes.Buffer
	if _, err := codec.WriteTo(&buf); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func BenchmarkReadBytes_Dict_10K(b *testing.B) {
	values := make([]uint32, 10_000)
	for i := range values {
		values[i] = uint32(i % 50)
	}
	data := serializeForRead(values, Options{})
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Load[uint32](data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReadBytes_RunEnd_10K(b *testing.B) {
	values := make([]uint32, 10_000)
	for i := range values {
		values[i] = uint32(i / 20)
	}
	data := serializeForRead(values, Options{})
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Load[uint32](data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReadBytes_Raw_10K(b *testing.B) {
	values := make([]uint32, 10_000)
	for i := range values {
		values[i] = uint32(i)
	}
	data := serializeForRead(values, Options{})
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Load[uint32](data); err != nil {
			b.Fatal(err)
		}
	}
}
