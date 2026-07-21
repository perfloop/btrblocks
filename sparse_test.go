package btrblocks

import (
	"math"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

type testSparseChildren[T Integer | Float] struct {
	testDictionaryChildren[T]
}

func (testSparseChildren[T]) BuildFill(child array.ArrayCore[T]) (EncodedArray[T], error) {
	return testRawChild(child)
}

type testStringSparseChildren struct {
	testStringDictionaryChildren
}

func (testStringSparseChildren) BuildFill(child array.ArrayCore[string]) (EncodedArray[string], error) {
	return testRawStringChild(child)
}

func TestSparseRoundTrip(t *testing.T) {
	values := []uint32{7, 7, 7, 3, 7, 7, 11, 7, 7}
	encoded, err := encodeIntegerSparse(array.NewPrimitivesUnsafe(values), testSparseChildren[uint32]{})
	require.NoError(t, err)
	require.Equal(t, CodecTypeSparse, encoded.CodecType())
	for i, want := range values {
		require.Equal(t, want, encoded.ValueAt(uint64(i)))
	}
	decoded, err := Decompress(encoded)
	require.NoError(t, err)
	require.Equal(t, values, decoded)

	loaded, err := LoadUnsigned[uint32](mustWriteEncodedArray(t, encoded))
	require.NoError(t, err)
	decoded, err = Decompress(loaded)
	require.NoError(t, err)
	require.Equal(t, values, decoded)
}

func TestSparsePreservesFloatNaNBits(t *testing.T) {
	values := []float64{math.NaN(), math.NaN(), 1}
	encoded, err := encodeFloatSparse(array.NewPrimitivesUnsafe(values), testSparseChildren[float64]{})
	require.NoError(t, err)
	decoded, err := Decompress(encoded)
	require.NoError(t, err)
	for i, want := range values {
		if math.Float64bits(decoded[i]) != math.Float64bits(want) {
			t.Fatalf("value %d = %v (%x), want %v (%x)", i, decoded[i], math.Float64bits(decoded[i]), want, math.Float64bits(want))
		}
	}
}

func TestSparseStringRoundTrip(t *testing.T) {
	values := []string{"fill", "fill", "patch", "fill", "other", "fill"}
	encoded, err := encodeStringSparse(mustStrings(t, values), testStringSparseChildren{})
	require.NoError(t, err)
	decoded, err := Decompress(encoded)
	require.NoError(t, err)
	require.Equal(t, values, decoded)
}

func TestSparseRejectsInvalidIndices(t *testing.T) {
	encoded := &sparseArray[uint32, uint8]{
		denseRows: 4,
		fill:      newRawArray(array.NewPrimitivesUnsafe([]uint32{0})),
		indices:   newRawArray(array.NewPrimitivesUnsafe([]uint8{1, 1})),
		values:    newRawArray(array.NewPrimitivesUnsafe([]uint32{2, 3})),
	}
	_, err := LoadUnsigned[uint32](mustWriteEncodedArray(t, encoded))
	require.ErrorContains(t, err, "not strictly increasing")
}
