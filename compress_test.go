package btrblocks

import (
	"bytes"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

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

func TestFloatSparseKeepsValuesRaw(t *testing.T) {
	codec, err := buildSparseCodec(array.NewPrimitivesUnsafe([]float64{0, 0, 0, 5.5, 0, 6.5, 0}), newPlanContext(Options{MaxDepth: 3}), 0, cmpFloats[float64])
	require.NoError(t, err)

	sparse, ok := codec.(*sparseCodec[float64, uint8])
	require.True(t, ok)
	require.IsType(t, &rawCodec[float64]{}, sparse.values)
}
