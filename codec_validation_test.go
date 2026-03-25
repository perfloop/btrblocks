package btrblocks

import (
	"bytes"
	"io"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

func mustWriteEncodedArray(t *testing.T, codec io.WriterTo) []byte {
	t.Helper()

	var buf bytes.Buffer
	_, err := codec.WriteTo(&buf)
	require.NoError(t, err)
	return buf.Bytes()
}

func TestCodeTypeValues(t *testing.T) {
	require.Equal(t, CodeType(0), CodecTypeUnknown)
	require.Equal(t, CodeType(1), CodecTypeConst)
	require.Equal(t, CodeType(2), CodecTypeRaw)
	require.Equal(t, CodeType(3), CodecTypeDict)
	require.Equal(t, CodeType(4), CodecTypeRunEnd)
	require.Equal(t, CodeType(5), CodecTypeZigZag)
	require.Equal(t, CodeType(6), CodecTypeBitpack)
	require.Equal(t, CodeType(7), CodecTypeFor)
	require.Equal(t, CodeType(9), CodecTypeSequence)
	require.Equal(t, CodeType(10), CodecTypeALP)
}

func TestReadRejectsRemovedCodecKind(t *testing.T) {
	var buf bytes.Buffer
	_, err := codecHeader{
		Version:  versionNumber,
		Kind:     CodeType(8),
		ElemType: PTypeUint32,
		Length:   1,
	}.WriteTo(&buf)
	require.NoError(t, err)

	_, err = Load[uint32](buf.Bytes())
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown kind = 8")
}

func TestReadRunEndRejectsZeroFirstEnd(t *testing.T) {
	data := mustWriteEncodedArray(t, &runEndArray[uint32, uint8]{
		length: 3,
		runs:   newRawArray(buildArray([]uint32{1, 2})),
		ends:   newRawArray(array.NewPrimitivesUnsafe([]uint8{0})),
	})

	_, err := Load[uint32](data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "runend first end = 0, want > 0")
}

func TestReadRunEndRejectsTerminalEndAtLength(t *testing.T) {
	data := mustWriteEncodedArray(t, &runEndArray[uint32, uint8]{
		length: 3,
		runs:   newRawArray(buildArray([]uint32{1, 2})),
		ends:   newRawArray(array.NewPrimitivesUnsafe([]uint8{3})),
	})

	_, err := Load[uint32](data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "runend last end = 3, want < 3")
}

func TestReadBitpackRejectsEmptyPatches(t *testing.T) {
	data := mustWriteEncodedArray(t, &bitPackedArray[uint32, uint8]{
		length:   3,
		bitWidth: 1,
		buf:      []byte{0},
		patches: &patches[uint32, uint8]{
			length:  3,
			indices: newRawArray(array.NewPrimitivesUnsafe([]uint8{})),
			values:  newRawArray(buildArray([]uint32{})),
		},
	})

	_, err := Load[uint32](data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "bitpack patches length = 0")
}

func TestReadBitpackRejectsOutOfRangePatchIndex(t *testing.T) {
	data := mustWriteEncodedArray(t, &bitPackedArray[uint32, uint8]{
		length:   3,
		bitWidth: 1,
		buf:      []byte{0},
		patches: &patches[uint32, uint8]{
			length:  3,
			indices: newRawArray(array.NewPrimitivesUnsafe([]uint8{3})),
			values:  newRawArray(buildArray([]uint32{42})),
		},
	})

	_, err := Load[uint32](data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "bitpack patch index = 3, want [0, 3)")
}

func TestReadBitpackKeepsPatchesEncodedUntilCopy(t *testing.T) {
	values := make([]uint32, 256)
	for i := range values {
		values[i] = uint32(i % 16)
	}
	values[17] = 1 << 20
	values[199] = 1<<21 + 3

	codec, err := buildBitPackedArray(array.NewPrimitivesUnsafe(values), newPlanContext(Options{}))
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	readEncodedArray, err := Load[uint32](buf.Bytes())
	require.NoError(t, err)

	bitpack, ok := readEncodedArray.(*bitPackedArray[uint32, uint8])
	if !ok {
		bitpack64, ok64 := readEncodedArray.(*bitPackedArray[uint32, uint64])
		require.True(t, ok64)
		require.NotNil(t, bitpack64.patches)
		require.Less(t, bitpack64.bitWidth, bitWidthForUnsigned(uint64(values[199])))

		require.Equal(t, values[0], bitpack64.ValueAt(0))
		require.Equal(t, values[17], bitpack64.ValueAt(17))
		require.Equal(t, values[199], bitpack64.ValueAt(199))

		decoded, err := Decompress(bitpack64)
		require.NoError(t, err)
		require.Equal(t, values, decoded)
		return
	}
	require.NotNil(t, bitpack.patches)
	require.Less(t, bitpack.bitWidth, bitWidthForUnsigned(uint64(values[199])))

	require.Equal(t, values[0], bitpack.ValueAt(0))
	require.Equal(t, values[17], bitpack.ValueAt(17))
	require.Equal(t, values[199], bitpack.ValueAt(199))

	decoded, err := Decompress(bitpack)
	require.NoError(t, err)
	require.Equal(t, values, decoded)
}

func TestReadALP64RejectsEmptyPatches(t *testing.T) {
	data := mustWriteEncodedArray(t, &alpArray[float64, int64, uint8]{decode: alpDecode64,
		length:  3,
		expE:    0,
		expF:    0,
		encoded: newRawArray(buildArray([]int64{1, 2, 3})),
		patches: &patches[float64, uint8]{
			length:  3,
			indices: newRawArray(array.NewPrimitivesUnsafe([]uint8{})),
			values:  newRawArray(buildArray([]float64{})),
		},
	})

	_, err := Load[float64](data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ALP patches length = 0")
}

func TestReadALP32RejectsOutOfRangePatchIndex(t *testing.T) {
	data := mustWriteEncodedArray(t, &alpArray[float32, int32, uint8]{decode: alpDecode32,
		length:  3,
		expE:    0,
		expF:    0,
		encoded: newRawArray(buildArray([]int32{1, 2, 3})),
		patches: &patches[float32, uint8]{
			length:  3,
			indices: newRawArray(array.NewPrimitivesUnsafe([]uint8{3})),
			values:  newRawArray(buildArray([]float32{1.25})),
		},
	})

	_, err := Load[float32](data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ALP patch index = 3, want [0, 3)")
}

func TestReadALP64KeepsPatchesEncodedUntilCopy(t *testing.T) {
	data := mustWriteEncodedArray(t, &alpArray[float64, int64, uint8]{decode: alpDecode64,
		length:  4,
		expE:    0,
		expF:    0,
		encoded: newRawArray(buildArray([]int64{1, 2, 3, 4})),
		patches: &patches[float64, uint8]{
			length:  4,
			indices: newRawArray(array.NewPrimitivesUnsafe([]uint8{1, 3})),
			values:  newRawArray(buildArray([]float64{20.5, 40.5})),
		},
	})

	readEncodedArray, err := Load[float64](data)
	require.NoError(t, err)

	codec := readEncodedArray.(*alpArray[float64, int64, uint8])
	require.NotNil(t, codec.patches)

	require.Equal(t, 1.0, codec.ValueAt(0))
	require.Equal(t, 20.5, codec.ValueAt(1))
	require.Equal(t, 3.0, codec.ValueAt(2))
	require.Equal(t, 40.5, codec.ValueAt(3))

	decoded, err := Decompress(codec)
	require.NoError(t, err)
	require.Equal(t, []float64{1.0, 20.5, 3.0, 40.5}, decoded)
}

func TestReadALP32KeepsPatchesEncodedUntilCopy(t *testing.T) {
	data := mustWriteEncodedArray(t, &alpArray[float32, int32, uint8]{decode: alpDecode32,
		length:  3,
		expE:    0,
		expF:    0,
		encoded: newRawArray(buildArray([]int32{1, 2, 3})),
		patches: &patches[float32, uint8]{
			length:  3,
			indices: newRawArray(array.NewPrimitivesUnsafe([]uint8{0, 2})),
			values:  newRawArray(buildArray([]float32{9.25, 7.5})),
		},
	})

	readEncodedArray, err := Load[float32](data)
	require.NoError(t, err)

	codec := readEncodedArray.(*alpArray[float32, int32, uint8])
	require.NotNil(t, codec.patches)

	require.Equal(t, float32(9.25), codec.ValueAt(0))
	require.Equal(t, float32(2.0), codec.ValueAt(1))
	require.Equal(t, float32(7.5), codec.ValueAt(2))

	decoded, err := Decompress(codec)
	require.NoError(t, err)
	require.Equal(t, []float32{9.25, 2.0, 7.5}, decoded)
}
