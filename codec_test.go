package btrblocks

import (
	"bytes"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

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

	bp := codec.(*bitPackedArray[uint32, uint64])
	require.NotNil(t, bp.patches)
	require.Less(t, bp.bitWidth, bitWidthForUnsigned(uint64(values[199])))

	assertRoundTrip(t, codec, values)
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
	codec := &alpArray[float64, int64, uint8]{decode: alpDecode64,
		length: 4, expE: 0, expF: 0,
		encoded: newRawArray(buildArray([]int64{1, 2, 3, 4})),
		patches: &patches[float64, uint8]{
			length:  4,
			indices: newRawArray(array.NewPrimitivesUnsafe([]uint8{1, 3})),
			values:  newRawArray(buildArray([]float64{20.5, 40.5})),
		},
	}
	assertRoundTrip(t, codec, []float64{1.0, 20.5, 3.0, 40.5})
}

func TestReadALP32KeepsPatchesEncodedUntilCopy(t *testing.T) {
	codec := &alpArray[float32, int32, uint8]{decode: alpDecode32,
		length: 3, expE: 0, expF: 0,
		encoded: newRawArray(buildArray([]int32{1, 2, 3})),
		patches: &patches[float32, uint8]{
			length:  3,
			indices: newRawArray(array.NewPrimitivesUnsafe([]uint8{0, 2})),
			values:  newRawArray(buildArray([]float32{9.25, 7.5})),
		},
	}
	assertRoundTrip(t, codec, []float32{9.25, 2.0, 7.5})
}

// --- Adversarial / corrupt data tests ---

func TestLoadRejectsInvalidData(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		_, err := Load[uint32](nil)
		require.Error(t, err)
	})
	t.Run("truncated header", func(t *testing.T) {
		_, err := Load[uint32](make([]byte, headerSize-1))
		require.Error(t, err)
	})
	t.Run("invalid version", func(t *testing.T) {
		data := mustWriteEncodedArray(t, newRawArray(buildArray([]uint32{1, 2, 3})))
		data[0] = 99
		_, err := Load[uint32](data)
		require.Error(t, err)
		require.Contains(t, err.Error(), "version")
	})
	t.Run("type mismatch", func(t *testing.T) {
		data := mustWriteEncodedArray(t, newRawArray(buildArray([]uint32{1, 2, 3})))
		_, err := Load[float64](data)
		require.Error(t, err)
		require.Contains(t, err.Error(), "element type")
	})
	t.Run("unknown codec kind", func(t *testing.T) {
		data := mustWriteEncodedArray(t, newRawArray(buildArray([]uint32{1, 2, 3})))
		data[1] = 255
		_, err := Load[uint32](data)
		require.Error(t, err)
		require.Contains(t, err.Error(), "unknown kind")
	})
	t.Run("nonzero reserved byte", func(t *testing.T) {
		data := mustWriteEncodedArray(t, newRawArray(buildArray([]uint32{1, 2, 3})))
		data[3] = 1
		_, err := Load[uint32](data)
		require.Error(t, err)
		require.Contains(t, err.Error(), "reserved")
	})
	t.Run("unsupported flags on raw", func(t *testing.T) {
		data := mustWriteEncodedArray(t, newRawArray(buildArray([]uint32{1, 2, 3})))
		data[4] = 0xff // flags byte
		_, err := Load[uint32](data)
		require.Error(t, err)
		require.Contains(t, err.Error(), "flags")
	})
}

func TestLoadRejectsTruncatedBody(t *testing.T) {
	t.Run("raw", func(t *testing.T) {
		data := mustWriteEncodedArray(t, newRawArray(buildArray([]uint32{1, 2, 3, 4, 5})))
		_, err := Load[uint32](data[:headerSize+2])
		require.Error(t, err)
	})
	t.Run("const", func(t *testing.T) {
		codec, err := newConstIntegerArray(array.NewPrimitivesUnsafe([]uint32{42, 42, 42}))
		require.NoError(t, err)
		data := mustWriteEncodedArray(t, codec)
		_, err = Load[uint32](data[:headerSize+1])
		require.Error(t, err)
	})
	t.Run("dict", func(t *testing.T) {
		codec, err := buildIntegerDictFromDistinct(buildArray([]uint32{1, 2, 1, 2, 1, 2}), nil, newPlanContext(Options{MaxDepth: 3}))
		require.NoError(t, err)
		data := mustWriteEncodedArray(t, codec)
		_, err = Load[uint32](data[:headerSize+1])
		require.Error(t, err)
	})
	t.Run("sequence", func(t *testing.T) {
		codec, err := newSequenceArray(buildArray([]uint32{10, 20, 30}))
		require.NoError(t, err)
		data := mustWriteEncodedArray(t, codec)
		_, err = Load[uint32](data[:headerSize+1])
		require.Error(t, err)
	})
	t.Run("runend", func(t *testing.T) {
		codec, err := buildRunEndArray(array.NewPrimitivesUnsafe([]uint32{5, 5, 5, 7, 7}), newPlanContext(Options{MaxDepth: 3}), cmpIntegers[uint32])
		require.NoError(t, err)
		data := mustWriteEncodedArray(t, codec)
		_, err = Load[uint32](data[:headerSize+1])
		require.Error(t, err)
	})
	t.Run("bitpack", func(t *testing.T) {
		values := make([]uint32, 64)
		for i := range values {
			values[i] = uint32(i % 8)
		}
		codec, err := buildBitPackedArray(array.NewPrimitivesUnsafe(values), newPlanContext(Options{}))
		require.NoError(t, err)
		data := mustWriteEncodedArray(t, codec)
		_, err = Load[uint32](data[:headerSize+1])
		require.Error(t, err)
	})
	t.Run("for", func(t *testing.T) {
		codec, err := buildFoRArray(array.NewPrimitivesUnsafe([]uint32{1000, 1001, 1002}), newPlanContext(Options{MaxDepth: 3}))
		require.NoError(t, err)
		data := mustWriteEncodedArray(t, codec)
		_, err = Load[uint32](data[:headerSize+1])
		require.Error(t, err)
	})
	t.Run("zigzag", func(t *testing.T) {
		codec, err := buildZigZagArray(buildArray([]int32{-1, -2, -3}), newPlanContext(Options{MaxDepth: 3}))
		require.NoError(t, err)
		data := mustWriteEncodedArray(t, codec)
		_, err = Load[int32](data[:headerSize+1])
		require.Error(t, err)
	})
	t.Run("alp", func(t *testing.T) {
		values := make([]float64, 64)
		for i := range values {
			values[i] = float64(i) * 0.01
		}
		codec, err := buildALPArray(array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}))
		require.NoError(t, err)
		data := mustWriteEncodedArray(t, codec)
		_, err = Load[float64](data[:headerSize+2])
		require.Error(t, err)
	})
	t.Run("fsst", func(t *testing.T) {
		values := make([]string, 50)
		for i := range values {
			values[i] = "test_string"
		}
		codec, err := buildFSSTArray(buildArray(values), newPlanContext(Options{MaxDepth: 3}))
		require.NoError(t, err)
		data := mustWriteEncodedArray(t, codec)
		_, err = Load[string](data[:headerSize+1])
		require.Error(t, err)
	})
	t.Run("alprd", func(t *testing.T) {
		values := make([]float64, 256)
		for i := range values {
			values[i] = 1.0 + float64(i)*1e-10
		}
		codec, err := buildALPRDArray[float64](array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}))
		require.NoError(t, err)
		data := mustWriteEncodedArray(t, codec)
		_, err = Load[float64](data[:headerSize+2])
		require.Error(t, err)
	})
}

// TestALPRDLoadRejectsMalformedBody exercises the ALPRD body parser against
// adversarial inputs: corrupt dictSize, truncated dict entries, and truncated
// bit-packed buffers. These target readALPRDArrayTyped which parses a single
// body blob by offset without per-field bounds checks.
func TestALPRDLoadRejectsMalformedBody(t *testing.T) {
	// Build valid ALPRD data as a baseline.
	values := make([]float64, 256)
	for i := range values {
		values[i] = 1.0 + float64(i)*1e-10
	}
	codec, err := buildALPRDArray[float64](array.NewPrimitivesUnsafe(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)
	valid := mustWriteEncodedArray(t, codec)

	// Verify baseline loads correctly.
	_, err = Load[float64](valid)
	require.NoError(t, err)

	t.Run("body too small for fixed fields", func(t *testing.T) {
		// ALPRD body needs at least 3 bytes (rightBW, leftBW, dictSize).
		// Corrupt NumBytes in the codec header to claim a 1-byte body.
		corrupted := append([]byte(nil), valid...)
		// NumBytes is at offset 16..23 in the codec header (little-endian uint64).
		corrupted[16] = 1
		corrupted[17] = 0
		corrupted[18] = 0
		corrupted[19] = 0
		corrupted[20] = 0
		corrupted[21] = 0
		corrupted[22] = 0
		corrupted[23] = 0
		_, err := Load[float64](corrupted)
		require.Error(t, err)
	})
	t.Run("dictSize exceeds max", func(t *testing.T) {
		// The 3rd body byte is dictSize. Set it to 255 (max is 8).
		corrupted := append([]byte(nil), valid...)
		corrupted[headerSize+2] = 255
		_, err := Load[float64](corrupted)
		require.Error(t, err)
		require.Contains(t, err.Error(), "dict size")
	})
}

func TestFSSTLoadRejectsMalformedBody(t *testing.T) {
	// Build valid FSST data as baseline.
	values := make([]string, 50)
	for i := range values {
		values[i] = "test_string"
	}
	codec, err := buildFSSTArray(buildArray(values), newPlanContext(Options{MaxDepth: 3}))
	require.NoError(t, err)
	valid := mustWriteEncodedArray(t, codec)

	_, err = Load[string](valid)
	require.NoError(t, err)

	t.Run("body too small", func(t *testing.T) {
		// Corrupt NumBytes to claim a 2-byte body (need >= 8).
		corrupted := append([]byte(nil), valid...)
		corrupted[16] = 2
		corrupted[17] = 0
		corrupted[18] = 0
		corrupted[19] = 0
		corrupted[20] = 0
		corrupted[21] = 0
		corrupted[22] = 0
		corrupted[23] = 0
		_, err := Load[string](corrupted)
		require.Error(t, err)
	})
	t.Run("table size exceeds body", func(t *testing.T) {
		// Set tableSize (first 4 bytes of body) to a huge value.
		corrupted := append([]byte(nil), valid...)
		// tableSize is at offset headerSize (start of body), little-endian uint32.
		corrupted[headerSize] = 0xff
		corrupted[headerSize+1] = 0xff
		corrupted[headerSize+2] = 0xff
		corrupted[headerSize+3] = 0x7f
		_, err := Load[string](corrupted)
		require.Error(t, err)
	})
}
