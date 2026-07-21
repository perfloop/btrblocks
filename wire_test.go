package btrblocks

import (
	"bytes"
	"fmt"
	"io"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/axiomhq/fsst"
	"github.com/stretchr/testify/require"
)

func mustWriteEncodedArray(t testing.TB, encoded io.WriterTo) []byte {
	t.Helper()
	var buf bytes.Buffer
	_, err := encoded.WriteTo(&buf)
	if err != nil {
		t.Fatalf("write encoded array: %v", err)
	}
	return buf.Bytes()
}

func assertRoundTrip[T Integer | Float | String](t *testing.T, encoded EncodedArray[T], want []T) {
	t.Helper()
	decoded, err := Decompress(encoded)
	if err != nil {
		t.Fatalf("decompress: %v", err)
	}
	assertValuesEqual(t, want, decoded)
	data := mustWriteEncodedArray(t, encoded)
	if got, want := uint64(len(data)), encoded.BinarySize(); got != want {
		t.Fatalf("encoded size: got %d want %d", got, want)
	}
}

func assertSliceRoundTrip[T Integer | Float | String](t *testing.T, encoded EncodedArray[T], start, end uint64, want []T) {
	t.Helper()
	sliced, err := encoded.Slice(start, end)
	if err != nil {
		t.Fatalf("slice [%d,%d): %v", start, end, err)
	}
	decoded, err := Decompress(sliced)
	if err != nil {
		t.Fatalf("decompress slice [%d,%d): %v", start, end, err)
	}
	assertValuesEqual(t, want[start:end], decoded)
}

func assertValuesEqual[T Integer | Float | String](t *testing.T, want, got []T) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("values length: got %d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] == want[i] {
			continue
		}
		// IEEE NaNs compare unequal to themselves. Treat two NaNs as equal here;
		// bit-preservation tests compare their representations explicitly.
		if got[i] != got[i] && want[i] != want[i] {
			continue
		}
		t.Fatalf("value %d: got %v want %v", i, got[i], want[i])
	}
}

func TestCodeTypeValues(t *testing.T) {
	require.Equal(t, CodecType(0), CodecTypeUnknown)
	require.Equal(t, CodecType(1), CodecTypeConst)
	require.Equal(t, CodecType(2), CodecTypeRaw)
	require.Equal(t, CodecType(3), CodecTypeDict)
	require.Equal(t, CodecType(4), CodecTypeRunEnd)
	require.Equal(t, CodecType(5), CodecTypeZigZag)
	require.Equal(t, CodecType(6), CodecTypeBitpack)
	require.Equal(t, CodecType(7), CodecTypeFor)
	require.Equal(t, CodecType(8), CodecTypeSparse)
	require.Equal(t, CodecType(9), CodecTypeSequence)
	require.Equal(t, CodecType(10), CodecTypeALP)
	require.Equal(t, CodecType(13), CodecTypeDelta)
	require.Equal(t, CodecType(14), CodecTypeNullable)
}

func TestReadRejectsRemovedCodecKind(t *testing.T) {
	var buf bytes.Buffer
	_, err := codecHeader{
		Version:  versionNumber,
		Type:     CodecType(15),
		ElemType: PTypeUint32,
		Length:   1,
	}.WriteTo(&buf)
	require.NoError(t, err)

	_, err = LoadUnsigned[uint32](buf.Bytes())
	require.Error(t, err)
	require.ErrorContains(t, err, "unknown kind = 15")
}

func TestReadRunEndRejectsZeroFirstEnd(t *testing.T) {
	data := mustWriteEncodedArray(t, &runEndArray[uint32, uint8]{
		denseRows: 3,
		runs:      newRawArray(buildArray([]uint32{1, 2})),
		ends:      newRawArray(array.NewPrimitivesUnsafe([]uint8{0})),
	})

	_, err := LoadUnsigned[uint32](data)
	require.Error(t, err)
	require.ErrorContains(t, err, "runend end = 0 at position 0, want > 0")
}

func TestReadRunEndRejectsTerminalEndAtLength(t *testing.T) {
	data := mustWriteEncodedArray(t, &runEndArray[uint32, uint8]{
		denseRows: 3,
		runs:      newRawArray(buildArray([]uint32{1, 2})),
		ends:      newRawArray(array.NewPrimitivesUnsafe([]uint8{3})),
	})

	_, err := LoadUnsigned[uint32](data)
	require.Error(t, err)
	require.ErrorContains(t, err, "runend end = 3 at position 0, want < 3")
}

func TestReadBitpackRejectsEmptyPatches(t *testing.T) {
	data := mustWriteEncodedArray(t, &bitPackedArray[uint32, uint8]{
		denseRows: 3,
		bitWidth:  1,
		buf:       []byte{0},
		patches: &patches[uint32, uint8]{
			length:  3,
			indices: newRawArray(array.NewPrimitivesUnsafe([]uint8{})),
			values:  newRawArray(buildArray([]uint32{})),
		},
	})

	_, err := LoadUnsigned[uint32](data)
	require.Error(t, err)
	require.ErrorContains(t, err, "bitpack patches length = 0")
}

func TestReadBitpackRejectsOutOfRangePatchIndex(t *testing.T) {
	data := mustWriteEncodedArray(t, &bitPackedArray[uint32, uint8]{
		denseRows: 3,
		bitWidth:  1,
		buf:       []byte{0},
		patches: &patches[uint32, uint8]{
			length:  3,
			indices: newRawArray(array.NewPrimitivesUnsafe([]uint8{3})),
			values:  newRawArray(buildArray([]uint32{42})),
		},
	})

	_, err := LoadUnsigned[uint32](data)
	require.Error(t, err)
	require.ErrorContains(t, err, "bitpack patch index = 3, want [0, 3)")
}

func TestPatchValidateRejectsInteriorOutOfRange(t *testing.T) {
	// Counterexample: indices [5, 1000, 6] with offset=5, length=10.
	// First (5) and last (6) are in range [5, 15), but interior index 1000
	// is out of range. The old validation only checked first and last.
	p := &patches[uint32, uint16]{
		length:  10,
		offset:  5,
		indices: newRawArray(array.NewPrimitivesUnsafe([]uint16{5, 1000, 6})),
		values:  newRawArray(buildArray([]uint32{1, 2, 3})),
	}
	err := p.Validate()
	require.Error(t, err)
	require.ErrorContains(t, err, "patch index = 1000")
}

func TestPatchValidateRejectsNonStrictlyIncreasing(t *testing.T) {
	p := &patches[uint32, uint8]{
		length:  10,
		offset:  0,
		indices: newRawArray(array.NewPrimitivesUnsafe([]uint8{2, 5, 3})),
		values:  newRawArray(buildArray([]uint32{1, 2, 3})),
	}
	err := p.Validate()
	require.Error(t, err)
	require.ErrorContains(t, err, "not strictly increasing")
}

func TestPatchValidateAcceptsValidPatches(t *testing.T) {
	p := &patches[uint32, uint8]{
		length:  10,
		offset:  0,
		indices: newRawArray(array.NewPrimitivesUnsafe([]uint8{1, 3, 7})),
		values:  newRawArray(buildArray([]uint32{10, 20, 30})),
	}
	require.NoError(t, p.Validate())
}

func TestReadBitpackKeepsPatchesEncodedUntilCopy(t *testing.T) {
	values := make([]uint32, 256)
	for i := range values {
		values[i] = uint32(i % 16)
	}
	values[17] = 1 << 20
	values[199] = 1<<21 + 3

	codec, err := encodeBitpack(array.NewPrimitivesUnsafe(values))
	require.NoError(t, err)

	bp := codec.(*bitPackedArray[uint32, uint64])
	require.True(t, bp.patches != nil)
	require.True(t, bp.bitWidth < bitWidthForUnsigned(uint64(values[199])))

	assertRoundTrip(t, codec, values)
}

func TestReadALP64RejectsEmptyPatches(t *testing.T) {
	data := mustWriteEncodedArray(t, &alpArray[float64, int64, uint8]{decode: alpDecode64,
		denseRows: 3,
		expE:      0,
		expF:      0,
		encoded:   newRawArray(buildArray([]int64{1, 2, 3})),
		patches: &patches[float64, uint8]{
			length:  3,
			indices: newRawArray(array.NewPrimitivesUnsafe([]uint8{})),
			values:  newRawArray(buildArray([]float64{})),
		},
	})

	_, err := LoadFloat64(data)
	require.Error(t, err)
	require.ErrorContains(t, err, "ALP patches length = 0")
}

func TestReadALP32RejectsOutOfRangePatchIndex(t *testing.T) {
	data := mustWriteEncodedArray(t, &alpArray[float32, int32, uint8]{decode: alpDecode32,
		denseRows: 3,
		expE:      0,
		expF:      0,
		encoded:   newRawArray(buildArray([]int32{1, 2, 3})),
		patches: &patches[float32, uint8]{
			length:  3,
			indices: newRawArray(array.NewPrimitivesUnsafe([]uint8{3})),
			values:  newRawArray(buildArray([]float32{1.25})),
		},
	})

	_, err := LoadFloat32(data)
	require.Error(t, err)
	require.ErrorContains(t, err, "ALP patch index = 3, want [0, 3)")
}

func TestReadALP64KeepsPatchesEncodedUntilCopy(t *testing.T) {
	codec := &alpArray[float64, int64, uint8]{decode: alpDecode64,
		denseRows: 4, expE: 0, expF: 0,
		encoded: newRawArray(buildArray([]int64{1, 2, 3, 4})),
		patches: &patches[float64, uint8]{
			length:  4,
			indices: newRawArray(array.NewPrimitivesUnsafe([]uint8{1, 3})),
			values:  newRawArray(buildArray([]float64{20.5, 40.5})),
		},
	}
	assertRoundTrip(t, codec, []float64{1.0, 20.5, 3.0, 40.5})
}

func TestLoadFSSTRejectsDecodedPayloadBeyondBudget(t *testing.T) {
	table := fsst.Train([][]byte{[]byte("example")})
	tableRaw, err := table.MarshalBinary()
	require.NoError(t, err)

	const codeBytes = 8<<20 + 1
	encoded := &fsstArray[uint32, uint32]{
		denseRows: 1,
		tableRaw:  tableRaw,
		codes:     make([]byte, codeBytes),
		offsets:   encodeRaw(array.NewPrimitivesUnsafe([]uint32{0, codeBytes})),
		lengths:   encodeRaw(array.NewPrimitivesUnsafe([]uint32{64<<20 + 1})),
		table:     table,
	}

	_, err = LoadStrings(mustWriteEncodedArray(t, encoded))
	require.ErrorContains(t, err, "decoded-byte limit")
}

func TestReadALP32KeepsPatchesEncodedUntilCopy(t *testing.T) {
	codec := &alpArray[float32, int32, uint8]{decode: alpDecode32,
		denseRows: 3, expE: 0, expF: 0,
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
		_, err := LoadUnsigned[uint32](nil)
		require.Error(t, err)
	})
	t.Run("truncated header", func(t *testing.T) {
		_, err := LoadUnsigned[uint32](make([]byte, headerSize-1))
		require.Error(t, err)
	})
	t.Run("invalid version", func(t *testing.T) {
		data := mustWriteEncodedArray(t, newRawArray(buildArray([]uint32{1, 2, 3})))
		data[0] = 99
		_, err := LoadUnsigned[uint32](data)
		require.Error(t, err)
		require.ErrorContains(t, err, "version")
	})
	t.Run("type mismatch", func(t *testing.T) {
		data := mustWriteEncodedArray(t, newRawArray(buildArray([]uint32{1, 2, 3})))
		_, err := LoadFloat64(data)
		require.Error(t, err)
		require.ErrorContains(t, err, "element type")
	})
	t.Run("unknown codec kind", func(t *testing.T) {
		data := mustWriteEncodedArray(t, newRawArray(buildArray([]uint32{1, 2, 3})))
		data[1] = 255
		_, err := LoadUnsigned[uint32](data)
		require.Error(t, err)
		require.ErrorContains(t, err, "unknown kind")
	})
	t.Run("nonzero reserved byte", func(t *testing.T) {
		data := mustWriteEncodedArray(t, newRawArray(buildArray([]uint32{1, 2, 3})))
		data[3] = 1
		_, err := LoadUnsigned[uint32](data)
		require.Error(t, err)
		require.ErrorContains(t, err, "reserved")
	})
	t.Run("unsupported flags on raw", func(t *testing.T) {
		data := mustWriteEncodedArray(t, newRawArray(buildArray([]uint32{1, 2, 3})))
		data[4] = 0xff // flags byte
		_, err := LoadUnsigned[uint32](data)
		require.Error(t, err)
		require.ErrorContains(t, err, "flags")
	})
}

func TestLoadRejectsTruncatedBody(t *testing.T) {
	t.Run("raw", func(t *testing.T) {
		data := mustWriteEncodedArray(t, newRawArray(buildArray([]uint32{1, 2, 3, 4, 5})))
		_, err := LoadUnsigned[uint32](data[:headerSize+2])
		require.ErrorContains(t, err, "raw body")
	})
	t.Run("const", func(t *testing.T) {
		codec, err := newConstIntegerArray(array.NewPrimitivesUnsafe([]uint32{42, 42, 42}))
		require.NoError(t, err)
		data := mustWriteEncodedArray(t, codec)
		_, err = LoadUnsigned[uint32](data[:headerSize+1])
		require.ErrorContains(t, err, "const body")
	})
	t.Run("dict", func(t *testing.T) {
		codec, err := buildIntegerDictTest(buildArray([]uint32{1, 2, 1, 2, 1, 2}))
		require.NoError(t, err)
		data := mustWriteEncodedArray(t, codec)
		_, err = LoadUnsigned[uint32](data[:headerSize+1])
		require.ErrorContains(t, err, "dict values")
	})
	t.Run("sequence", func(t *testing.T) {
		codec, err := newSequenceArray(buildArray([]uint32{10, 20, 30}))
		require.NoError(t, err)
		data := mustWriteEncodedArray(t, codec)
		_, err = LoadUnsigned[uint32](data[:headerSize+1])
		require.ErrorContains(t, err, "sequence base")
	})
	t.Run("runend", func(t *testing.T) {
		codec, err := buildPrimitiveRunEndTest(array.NewPrimitivesUnsafe([]uint32{5, 5, 5, 7, 7}), array.CmpIntegers[uint32])
		require.NoError(t, err)
		data := mustWriteEncodedArray(t, codec)
		_, err = LoadUnsigned[uint32](data[:headerSize+1])
		require.ErrorContains(t, err, "runend runs")
	})
	t.Run("bitpack", func(t *testing.T) {
		values := make([]uint32, 64)
		for i := range values {
			values[i] = uint32(i % 8)
		}
		codec, err := encodeBitpack(array.NewPrimitivesUnsafe(values))
		require.NoError(t, err)
		data := mustWriteEncodedArray(t, codec)
		_, err = LoadUnsigned[uint32](data[:headerSize+1])
		require.ErrorContains(t, err, "bitpack packed values")
	})
	t.Run("for", func(t *testing.T) {
		codec, err := encodeFoR(array.NewPrimitivesUnsafe([]uint32{1000, 1001, 1002}))
		require.NoError(t, err)
		data := mustWriteEncodedArray(t, codec)
		_, err = LoadUnsigned[uint32](data[:headerSize+1])
		require.ErrorContains(t, err, "for minimum")
	})
	t.Run("delta", func(t *testing.T) {
		codec, err := buildDeltaTest(array.NewPrimitivesUnsafe([]uint32{1000, 1005, 1010}))
		require.NoError(t, err)
		data := mustWriteEncodedArray(t, codec)
		_, err = LoadUnsigned[uint32](data[:headerSize+1])
		require.ErrorContains(t, err, "delta base")
	})
	t.Run("zigzag", func(t *testing.T) {
		codec, err := buildZigZagTest(buildArray([]int32{-1, -2, -3}))
		require.NoError(t, err)
		data := mustWriteEncodedArray(t, codec)
		_, err = LoadSigned[int32](data[:headerSize+1])
		require.ErrorContains(t, err, "zigzag child header")
	})
	t.Run("alp", func(t *testing.T) {
		values := make([]float64, 64)
		for i := range values {
			values[i] = float64(i) * 0.01
		}
		codec, err := buildALP64Test(array.NewPrimitivesUnsafe(values))
		require.NoError(t, err)
		data := mustWriteEncodedArray(t, codec)
		_, err = LoadFloat64(data[:headerSize+2])
		require.ErrorContains(t, err, "ALP encoded child")
	})
	t.Run("fsst", func(t *testing.T) {
		values := make([]string, 50)
		for i := range values {
			values[i] = "test_string"
		}
		codec, err := buildFSSTTest(mustStrings(t, values))
		require.NoError(t, err)
		data := mustWriteEncodedArray(t, codec)
		_, err = LoadStrings(data[:headerSize+1])
		require.ErrorContains(t, err, "fsst table size")
	})
	t.Run("alprd", func(t *testing.T) {
		values := make([]float64, 256)
		for i := range values {
			values[i] = 1.0 + float64(i)*1e-10
		}
		codec, err := buildALPRD64Test(array.NewPrimitivesUnsafe(values))
		require.NoError(t, err)
		data := mustWriteEncodedArray(t, codec)
		_, err = LoadFloat64(data[:headerSize+2])
		require.ErrorContains(t, err, "ALPRD body")
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
	codec, err := buildALPRD64Test(array.NewPrimitivesUnsafe(values))
	require.NoError(t, err)
	valid := mustWriteEncodedArray(t, codec)

	// Verify baseline loads correctly.
	_, err = LoadFloat64(valid)
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
		_, err := LoadFloat64(corrupted)
		require.Error(t, err)
	})
	t.Run("dictSize exceeds max", func(t *testing.T) {
		// The 3rd body byte is dictSize. Set it to 255 (max is 8).
		corrupted := append([]byte(nil), valid...)
		corrupted[headerSize+2] = 255
		_, err := LoadFloat64(corrupted)
		require.Error(t, err)
		require.ErrorContains(t, err, "dict size")
	})
}

func TestDictDecompressRejectsOutOfRangeOrdinal(t *testing.T) {
	// Construct a dict with ordinals that exceed the values length.
	// 2 values [10, 20], but ordinal indices [0, 5, 1] — ordinal 5 is OOB.
	d := &dictArray[uint32, uint8]{
		values:  newRawArray(buildArray([]uint32{10, 20})),
		indices: newRawArray(array.NewPrimitivesUnsafe([]uint8{0, 5, 1})),
	}
	dst := make([]uint32, 3)
	err := d.DecompressInto(dst)
	require.Error(t, err)
	require.ErrorContains(t, err, "dict ordinal")
}

func TestRunEndValidateRejectsUnsortedEnds(t *testing.T) {
	// Construct a runend array with non-increasing ends [3, 1, 5].
	data := mustWriteEncodedArray(t, &runEndArray[uint32, uint8]{
		denseRows: 10,
		runs:      newRawArray(buildArray([]uint32{1, 2, 3, 4})),
		ends:      newRawArray(array.NewPrimitivesUnsafe([]uint8{3, 1, 5})),
	})

	_, err := LoadUnsigned[uint32](data)
	require.Error(t, err)
	require.ErrorContains(t, err, "not strictly increasing")
}

func TestFSSTDecompressRejectsOutOfRangeOffsets(t *testing.T) {
	// Construct an fsstArray with offsets that point past the codes buffer.
	table := &fsst.Table{} // identity table — codes pass through unchanged

	f := &fsstArray[uint8, uint8]{
		denseRows: 2,
		tableRaw:  mustMarshalTable(table),
		codes:     []byte("helloworld"),                                       // 10 bytes
		offsets:   newRawArray(array.NewPrimitivesUnsafe([]uint8{0, 5, 200})), // offset 200 > len(codes)
		lengths:   newRawArray(array.NewPrimitivesUnsafe([]uint8{5, 5})),
		table:     table,
	}
	dst := make([]string, 2)
	err := f.DecompressInto(dst)
	require.Error(t, err)
	require.ErrorContains(t, err, "fsst offset")
}

func mustMarshalTable(t *fsst.Table) []byte {
	data, err := t.MarshalBinary()
	if err != nil {
		panic(err)
	}
	return data
}

func TestFSSTLoadRejectsMalformedBody(t *testing.T) {
	// Build valid FSST data as baseline.
	values := make([]string, 50)
	for i := range values {
		values[i] = "test_string"
	}
	codec, err := buildFSSTTest(mustStrings(t, values))
	require.NoError(t, err)
	valid := mustWriteEncodedArray(t, codec)

	_, err = LoadStrings(valid)
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
		_, err := LoadStrings(corrupted)
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
		_, err := LoadStrings(corrupted)
		require.Error(t, err)
	})
}

func TestFSSTLoadRejectsPlausibleWrongLength(t *testing.T) {
	encoded, err := buildFSSTTest(mustStrings(t, []string{"hello"}))
	require.NoError(t, err)
	fsstEncoded, ok := encoded.(*fsstArray[uint8, uint8])
	require.True(t, ok)
	fsstEncoded.lengths = newRawArray(array.NewPrimitivesUnsafe([]uint8{4}))

	_, err = LoadStrings(mustWriteEncodedArray(t, fsstEncoded))
	require.ErrorContains(t, err, "fsst decoded length")
}

func TestLoadRejectsTrailingDataAndPrefixLoadConsumesOneArray(t *testing.T) {
	encoded := encodeRaw(array.NewPrimitivesUnsafe([]uint32{1, 2, 3}))
	data := append(mustWriteEncodedArray(t, encoded), 0xff)

	_, err := LoadUnsigned[uint32](data)
	require.ErrorContains(t, err, "trailing data")

	br := array.BufReader{Buf: data}
	loaded, err := LoadUnsignedFromBuf[uint32](&br)
	require.NoError(t, err)
	require.Equal(t, encoded.BinarySize(), uint64(br.Off))
	require.Equal(t, 1, br.Remaining())
	require.Equal(t, uint32(3), loaded.ValueAt(2))
}

// BenchmarkWriteVirtualArray measures the chunked virtual-array writer used by
// FoR, ZigZag, and ALP.
func BenchmarkWriteVirtualArray(b *testing.B) {
	for _, n := range []int{10_000, 100_000} {
		values := make([]uint32, n)
		for i := range values {
			values[i] = 1_000_000 + uint32(i%64)
		}
		codec, err := encodeFoR(array.NewPrimitivesUnsafe(values))
		if err != nil {
			b.Fatal(err)
		}

		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := codec.WriteTo(io.Discard); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// TestReadRejectsExcessiveNestingDepth guards the decode path against
// input-driven recursion: a stream of nested dict headers must fail with an
// error instead of overflowing the stack.
func TestReadRejectsExcessiveNestingDepth(t *testing.T) {
	var buf bytes.Buffer
	h := codecHeader{Version: versionNumber, Type: CodecTypeDict, ElemType: PTypeInt64, Length: 1}
	for range 64 {
		_, err := h.WriteTo(&buf)
		require.NoError(t, err)
	}
	_, err := LoadSigned[int64](buf.Bytes())
	require.ErrorContains(t, err, "nesting depth")
}
