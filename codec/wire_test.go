package codec

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"runtime"
	"testing"
	"time"

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

func mustStrings(t testing.TB, values []string) array.Array[string] {
	t.Helper()
	result, err := array.NewStrings(values)
	if err != nil {
		t.Fatal(err)
	}
	return result
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
	require.Equal(t, CodecType(11), CodecTypeFSST)
	require.Equal(t, CodecType(12), CodecTypeALPRD)
	require.Equal(t, CodecType(13), CodecTypeDelta)
	require.Equal(t, CodecType(14), CodecTypeNullable)
}

func TestV1RawUint32GoldenBytes(t *testing.T) {
	const goldenHex = "010207000000000003000000000000002000000000000000" +
		"0107000003000000000000000c00000000000000" +
		"010000000200000003000000"
	want, err := hex.DecodeString(goldenHex)
	if err != nil {
		t.Fatalf("decode golden stream: %v", err)
	}
	encoded, err := EncodeRaw(array.NewPrimitives([]uint32{1, 2, 3}))
	if err != nil {
		t.Fatalf("EncodeRaw: %v", err)
	}
	got := mustWriteEncodedArray(t, encoded)
	if !bytes.Equal(got, want) {
		t.Fatalf("v1 raw stream = %x, want %x", got, want)
	}
	loaded, err := LoadUnsigned[uint32](want)
	if err != nil {
		t.Fatalf("LoadUnsigned golden stream: %v", err)
	}
	decoded, err := Decompress(loaded)
	if err != nil {
		t.Fatalf("Decompress golden stream: %v", err)
	}
	if fmt.Sprint(decoded) != "[1 2 3]" {
		t.Fatalf("golden values = %v, want [1 2 3]", decoded)
	}
}

func FuzzLoadEncodedBytesAllTypes(f *testing.F) {
	primitive, err := EncodeRaw(array.NewPrimitives([]uint32{1, 2, 3}))
	if err != nil {
		f.Fatalf("EncodeRaw primitive seed: %v", err)
	}
	stringsSeed, err := EncodeRaw(mustStrings(f, []string{"", "alpha", "beta"}))
	if err != nil {
		f.Fatalf("EncodeRaw string seed: %v", err)
	}
	f.Add(mustWriteEncodedArray(f, primitive))
	f.Add(mustWriteEncodedArray(f, stringsSeed))
	f.Add([]byte(nil))
	f.Add(make([]byte, HeaderSize))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		opts := ReadOptions{
			MaxLength:       1 << 12,
			MaxBytes:        1 << 20,
			MaxDecodedBytes: 1 << 20,
			MaxDepth:        8,
		}
		exerciseLoad(t, data, opts, LoadSigned[int8])
		exerciseLoad(t, data, opts, LoadSigned[int16])
		exerciseLoad(t, data, opts, LoadSigned[int32])
		exerciseLoad(t, data, opts, LoadSigned[int64])
		exerciseLoad(t, data, opts, LoadUnsigned[uint8])
		exerciseLoad(t, data, opts, LoadUnsigned[uint16])
		exerciseLoad(t, data, opts, LoadUnsigned[uint32])
		exerciseLoad(t, data, opts, LoadUnsigned[uint64])
		exerciseLoad(t, data, opts, LoadFloat32)
		exerciseLoad(t, data, opts, LoadFloat64)
		exerciseLoad(t, data, opts, LoadStrings)
	})
}

// valueAtSamples bounds how many offsets exerciseLoad reads individually.
// ValueAt is O(offset) in the delta and run-end codecs, so scanning every offset
// would cost O(n^2) for evidence a spread of offsets already gives — the same
// reason the read path validates with a sequential pass (see readBudget).
const valueAtSamples = 64

// exerciseLoad loads data and, when it decodes, drives the access path that
// Load deliberately defers validation to. Load range-checks only what costs
// O(1) per element; Decompress, DecompressInto, ValueAt and Slice are where the
// rest of a crafted tree's claims are finally tested, so a fuzz target that
// stops at Load never reaches them. Refusing to decode is a valid answer here —
// panicking, or a tree disagreeing with itself, is not.
func exerciseLoad[T Integer | Float | String](t *testing.T, data []byte, opts ReadOptions, load func([]byte, ...ReadOptions) (EncodedArray[T], error)) {
	encoded, err := load(data, opts)
	if err != nil {
		return
	}
	values, err := Decompress(encoded)
	if err != nil {
		return
	}
	length := encoded.Length()
	if uint64(len(values)) != length {
		t.Fatalf("decompressed %d values, want length %d", len(values), length)
	}

	into := make([]T, length)
	if err := encoded.DecompressInto(into); err != nil {
		t.Fatalf("DecompressInto after a successful Decompress: %v", err)
	}
	assertValuesEqual(t, values, into)

	sampled := make([]T, 0, valueAtSamples+1)
	want := make([]T, 0, valueAtSamples+1)
	stride := max(length/valueAtSamples, 1)
	for offset := uint64(0); offset < length; offset += stride {
		if !encoded.IsValid(offset) && encoded.NullCount() == 0 {
			t.Fatalf("offset %d is null in an array reporting no nulls", offset)
		}
		sampled = append(sampled, encoded.ValueAt(offset))
		want = append(want, values[offset])
	}
	assertValuesEqual(t, want, sampled)

	start := length / 2
	sliced, err := encoded.Slice(start, length)
	if errors.Is(err, ErrMaterializationLimit) {
		// A codec that cannot slice structurally decodes the whole node under
		// the limit this stream was loaded with, which is tighter than the
		// ambient one Decompress just answered under. Refusing is valid.
		return
	}
	if err != nil {
		t.Fatalf("Slice [%d, %d): %v", start, length, err)
	}
	slicedValues, err := Decompress(sliced)
	if err != nil {
		t.Fatalf("Decompress slice [%d, %d): %v", start, length, err)
	}
	assertValuesEqual(t, values[start:], slicedValues)
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
	err := p.validateIndices([]uint16{5, 1000, 6})
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
	err := p.validateIndices([]uint8{2, 5, 3})
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
	require.NoError(t, p.validate())
	require.NoError(t, p.validateIndices([]uint8{1, 3, 7}))
}

func TestReadBitpackKeepsPatchesEncodedUntilCopy(t *testing.T) {
	values := make([]uint32, 256)
	for i := range values {
		values[i] = uint32(i % 16)
	}
	values[17] = 1 << 20
	values[199] = 1<<21 + 3

	codec, err := encodeBitpack(array.NewPrimitivesUnsafe(values), testBuildBudget)
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
		codec, err := encodeBitpack(array.NewPrimitivesUnsafe(values), testBuildBudget)
		require.NoError(t, err)
		data := mustWriteEncodedArray(t, codec)
		_, err = LoadUnsigned[uint32](data[:headerSize+1])
		require.ErrorContains(t, err, "bitpack packed values")
	})
	t.Run("for", func(t *testing.T) {
		codec, err := encodeFoR(array.NewPrimitivesUnsafe([]uint32{1000, 1001, 1002}), testBuildBudget)
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
		codec, err := encodeFoR(array.NewPrimitivesUnsafe(values), testBuildBudget)
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

// --- Decode-work regression tests ---
//
// Each stream below is a few dozen bytes that declares a length a child codec
// expands from almost nothing. Before the read path was charged, validating
// them walked the child with ValueAt, whose cost is O(offset) in the delta
// codec: the run-end stream took 16.65s and was then accepted, the nullable one
// 16.04s before it was rejected. Every one of them must now settle in the time
// a single sequential pass takes.

// maxDecodeAnswer bounds how long one of those decodes may take to answer. They
// answer in single-digit milliseconds now and took 16s before, so a bound in
// between fails loudly on a regression without being flaky on a busy machine.
const maxDecodeAnswer = 2 * time.Second

// assertAnsweredWithin requires work to finish within maxDecodeAnswer and
// returns its error for the caller to classify: an answer given slowly is a
// regression even when it is the right answer.
//
// The work runs on its own goroutine so the bound is on the wait, not on a
// measurement taken afterwards. Uncharged validation of these streams does not
// take a few seconds, it takes hours; a test that timed the call and checked
// the elapsed time after it returned would hang until the whole package's
// timeout instead of reporting the regression. The abandoned goroutine outlives
// the failure, which only happens in a run that has already failed.
func assertAnsweredWithin(t *testing.T, work func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- work() }()
	select {
	case err := <-done:
		return err
	case <-time.After(maxDecodeAnswer):
		t.Fatalf("no answer within %s", maxDecodeAnswer)
		return nil
	}
}

// assertRejectsWithin additionally requires the answer to be a rejection.
func assertRejectsWithin(t *testing.T, load func() error) error {
	t.Helper()
	err := assertAnsweredWithin(t, load)
	require.Error(t, err)
	return err
}

func codecNode(t *testing.T, h codecHeader, parts ...[]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	_, err := h.WriteTo(&buf)
	require.NoError(t, err)
	for _, part := range parts {
		buf.Write(part)
	}
	return buf.Bytes()
}

// sequenceNode declares length elements of an arithmetic progression in two
// inline scalars, the cheapest way to buy an enormous logical length.
func sequenceNode[T Integer](t *testing.T, length uint64, base, step T) []byte {
	t.Helper()
	var body bytes.Buffer
	_, err := writeIntegerLE(&body, base)
	require.NoError(t, err)
	_, err = writeIntegerLE(&body, step)
	require.NoError(t, err)
	return codecNode(t, codecHeader{
		Version:  versionNumber,
		Type:     CodecTypeSequence,
		ElemType: array.PTypeOfPrimitive[T](),
		Length:   length,
		NumBytes: uint64(body.Len()),
	}, body.Bytes())
}

// dictNode declares length ordinals over values. One level costs about a
// hundred bytes and buys a full scan of its own declared length, so nesting
// them is the cheapest way to ask a decoder for unbounded validation work.
func dictNode(t *testing.T, elem PType, length uint64, values, indices []byte) []byte {
	t.Helper()
	return codecNode(t, codecHeader{
		Version:  versionNumber,
		Type:     CodecTypeDict,
		ElemType: elem,
		Length:   length,
	}, values, indices)
}

// deltaNode wraps child, whose ValueAt costs O(offset), in a prefix sum.
func deltaNode[T Integer](t *testing.T, childLength uint64, base T, child []byte) []byte {
	t.Helper()
	var body bytes.Buffer
	_, err := writeIntegerLE(&body, base)
	require.NoError(t, err)
	return codecNode(t, codecHeader{
		Version:  versionNumber,
		Type:     CodecTypeDelta,
		ElemType: array.PTypeOfPrimitive[T](),
		Length:   childLength + 1,
		NumBytes: uint64(body.Len()),
	}, body.Bytes(), child)
}

func TestLoadRejectsRunEndOverDeltaSequence(t *testing.T) {
	// 16M run ends, each of which the old validation loop reached by summing
	// every delta before it.
	const ends = 1 << 24
	stream := codecNode(t, codecHeader{
		Version:  versionNumber,
		Type:     CodecTypeRunEnd,
		ElemType: PTypeUint64,
		Length:   ends + 1,
	},
		sequenceNode[uint64](t, ends+1, 0, 1),
		deltaNode[uint64](t, ends-1, 1, sequenceNode[uint64](t, ends-1, 1, 0)),
	)
	require.Less(t, len(stream), 200)

	err := assertRejectsWithin(t, func() error {
		_, err := LoadUnsigned[uint64](stream)
		return err
	})
	require.ErrorIs(t, err, ErrMaterializationLimit)
}

func TestLoadRejectsNestedDictSequence(t *testing.T) {
	// Eight dictionaries, each declaring a full-length ordinal child over the
	// next, in a few hundred bytes: the work the tree asks for is the product of
	// its declared lengths, and only the budget bounds it.
	const length = defaultMaxReadLength
	stream := sequenceNode[uint64](t, 256, 1000, 1)
	for range 8 {
		stream = dictNode(t, PTypeUint64, length, stream, sequenceNode[uint8](t, length, 0, 0))
	}
	require.Less(t, len(stream), 1024)

	err := assertRejectsWithin(t, func() error {
		_, err := LoadUnsigned[uint64](stream, ReadOptions{MaxWork: 1 << 16})
		return err
	})
	require.ErrorIs(t, err, ErrWorkLimit)
}

// TestLoadRejectsNestedDictOverOversizedValues pins the charge on the whole
// subtree a scan materializes rather than on the scanned node's declared
// length. A dictionary's Length() is its ordinal child's, so each level here
// measures one element while its values child measures 64 MiB, and each level
// is the next one's ordinal child. Charging the declared length billed 28
// visits for 28 full 64 MiB decodes: accepted, seconds later, with every
// level's buffer still live under the one below it.
func TestLoadRejectsNestedDictOverOversizedValues(t *testing.T) {
	// A values child of the whole decode budget leaves no room for the node's
	// own destination, so the first scan of an enclosing level is refused
	// before it allocates.
	const values = defaultMaxReadLength
	stream := sequenceNode[uint8](t, 1, 0, 0)
	for range 28 {
		stream = dictNode(t, PTypeUint8, 1, sequenceNode[uint8](t, values, 0, 1), stream)
	}
	require.Less(t, len(stream), 2048)

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	err := assertRejectsWithin(t, func() error {
		_, err := LoadUnsigned[uint8](stream)
		return err
	})
	runtime.ReadMemStats(&after)
	require.ErrorIs(t, err, ErrMaterializationLimit)
	require.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(16<<20), "rejected, but only after materializing the nest")
}

// assertRejectedWithoutDecoding runs load and requires that the stream was
// rejected before any large child was decoded: the allocation delta stays small
// and the error names the guard. Allocation is asserted first so a regression to
// a post-decode check reports the megabytes rather than a string mismatch, and
// the load runs synchronously so no worker goroutine's late allocation can
// misattribute to another test under -shuffle.
func assertRejectedWithoutDecoding(t *testing.T, load func() error, wantErr string) {
	t.Helper()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	err := load()
	runtime.ReadMemStats(&after)
	require.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(1<<20), "rejected only after decoding a child")
	require.ErrorContains(t, err, wantErr)
}

// TestLoadRejectsOversizedOrdinalChild verifies that a run-end or sparse node
// whose ordinal/index child declares more elements than the node's row count
// AND its index type could ever hold is rejected from the two headers alone,
// before the child subtree is decoded. A strictly increasing child of type I
// within [0, length) has at most min(length, maxValue(I)+1) elements; the type
// term is the load-bearing half, since a uint8 child holds at most 256 values
// no matter how large the declared parent length is.
func TestLoadRejectsOversizedOrdinalChild(t *testing.T) {
	const big = 8 << 20
	cases := []struct {
		name    string
		stream  []byte
		wantErr string
	}{
		{
			// Narrow ordinal type over a large parent: 8M uint8 ends declared for a
			// 1<<26-row node. 8M < 1<<26, so a parent-only bound would pass, but a
			// uint8 sequence in (0, length) holds at most 255 values — the case the
			// earlier parent-relative guard let through at a full decode.
			name: "runend narrow ordinal type",
			stream: codecNode(t, codecHeader{Version: versionNumber, Type: CodecTypeRunEnd, ElemType: PTypeUint64, Length: 1 << 26},
				sequenceNode[uint64](t, big+1, 0, 1),
				sequenceNode[uint8](t, big, 1, 1)),
			wantErr: "runend ordinal count",
		},
		{
			name: "sparse child exceeds parent",
			stream: codecNode(t, codecHeader{Version: versionNumber, Type: CodecTypeSparse, ElemType: PTypeUint64, Length: 1},
				sequenceNode[uint64](t, 1, 7, 0),
				sequenceNode[uint64](t, big, 0, 1),
				sequenceNode[uint64](t, big, 0, 1)),
			wantErr: "sparse index count",
		},
		{
			name: "sparse narrow index type",
			stream: codecNode(t, codecHeader{Version: versionNumber, Type: CodecTypeSparse, ElemType: PTypeUint64, Length: 1 << 26},
				sequenceNode[uint64](t, 1, 7, 0),
				sequenceNode[uint8](t, big, 0, 1),
				sequenceNode[uint64](t, big, 0, 1)),
			wantErr: "sparse index count",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Less(t, len(tc.stream), 300)
			assertRejectedWithoutDecoding(t, func() error {
				_, err := LoadUnsigned[uint64](tc.stream)
				return err
			}, tc.wantErr)
		})
	}
}

// TestLoadRejectsOversizedSiblingChild covers the node's *value* children
// (run-end runs, sparse fill and values), read through the generic value reader
// rather than a typed header. Each is a dictionary declaring 8M rows — a codec
// whose construction decodes its ordinal child — so without the header peek each
// would allocate ~64 MiB before a later structural check discarded it. A run
// covers >=1 row, a fill is a single value, and exceptions pair with indices, so
// all three are refutable from the headers.
func TestLoadRejectsOversizedSiblingChild(t *testing.T) {
	const big = 8 << 20
	// A dictionary of `big` rows over a 256-entry table; loading it decodes the
	// full ordinal child.
	bigDict := func() []byte {
		return dictNode(t, PTypeUint64, big, sequenceNode[uint64](t, 256, 0, 1), sequenceNode[uint64](t, big, 0, 0))
	}
	cases := []struct {
		name    string
		stream  []byte
		wantErr string
	}{
		{
			name: "runend oversized runs",
			stream: codecNode(t, codecHeader{Version: versionNumber, Type: CodecTypeRunEnd, ElemType: PTypeUint64, Length: 2},
				bigDict(),
				sequenceNode[uint64](t, 1, 1, 1)),
			wantErr: "runend run count",
		},
		{
			name: "sparse oversized fill",
			stream: codecNode(t, codecHeader{Version: versionNumber, Type: CodecTypeSparse, ElemType: PTypeUint64, Length: 4},
				bigDict(),
				sequenceNode[uint8](t, 2, 0, 1),
				sequenceNode[uint64](t, 2, 0, 1)),
			wantErr: "sparse fill length",
		},
		{
			name: "sparse oversized values",
			stream: codecNode(t, codecHeader{Version: versionNumber, Type: CodecTypeSparse, ElemType: PTypeUint64, Length: 4},
				sequenceNode[uint64](t, 1, 7, 0),
				sequenceNode[uint8](t, 2, 0, 1),
				bigDict()),
			wantErr: "sparse values length",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertRejectedWithoutDecoding(t, func() error {
				_, err := LoadUnsigned[uint64](tc.stream)
				return err
			}, tc.wantErr)
		})
	}
}

// TestLoadAcceptsBoundaryOrdinalChild pins the other side of the bound: a child
// exactly at the maximum count a valid stream can produce must load. This is
// what keeps the header bound from over-rejecting real encoder output, and it
// guards the operator (`>`, not `>=`) — tightening either check by one would
// fail here.
func TestLoadAcceptsBoundaryOrdinalChild(t *testing.T) {
	t.Run("runend max runs", func(t *testing.T) {
		// Every value distinct → every row starts a run → ends.Length() == n-1,
		// the largest an n-row run-end node can hold, with the ends exactly filling
		// the uint8 range (1..255).
		const n = 256
		values := make([]uint32, n)
		for i := range values {
			values[i] = uint32(i)
		}
		encoded, err := buildPrimitiveRunEndTest(array.NewPrimitivesUnsafe(values), array.CmpIntegers[uint32])
		require.NoError(t, err)
		require.Equal(t, CodecTypeRunEnd, encoded.CodecType())
		loaded, err := LoadUnsigned[uint32](mustWriteEncodedArray(t, encoded))
		require.NoError(t, err)
		decoded, err := Decompress(loaded)
		require.NoError(t, err)
		require.Equal(t, values, decoded)
	})

	t.Run("sparse all exceptions", func(t *testing.T) {
		// A fill value present in no row makes every row an exception, so
		// indices.Length() == length — the boundary the sparse bound permits with
		// `>`. Built directly because the value-based encoder never selects a fill
		// that appears zero times.
		const n = 256
		idx := make([]uint8, n)
		vals := make([]uint32, n)
		for i := range idx {
			idx[i] = uint8(i)
			vals[i] = uint32(i) + 1000
		}
		encoded := &sparseArray[uint32, uint8]{
			denseRows: n,
			fill:      newRawArray(array.NewPrimitivesUnsafe([]uint32{7})),
			indices:   newRawArray(array.NewPrimitivesUnsafe(idx)),
			values:    newRawArray(array.NewPrimitivesUnsafe(vals)),
		}
		loaded, err := LoadUnsigned[uint32](mustWriteEncodedArray(t, encoded))
		require.NoError(t, err)
		decoded, err := Decompress(loaded)
		require.NoError(t, err)
		require.Equal(t, vals, decoded)
	})
}

func TestLoadRejectsNullableWithDeltaSequenceValidity(t *testing.T) {
	// A 1M-row bitmap is 128 KiB of validity bytes, every one of which the old
	// loop reached through the whole prefix sum before it.
	const length = 1 << 20
	const validityBytes = length / 8
	var nullCount [nullableBodySize]byte
	binary.LittleEndian.PutUint64(nullCount[:], 1)
	stream := codecNode(t, codecHeader{
		Version:  versionNumber,
		Type:     CodecTypeNullable,
		ElemType: PTypeUint64,
		Length:   length,
		NumBytes: nullableBodySize,
	},
		nullCount[:],
		sequenceNode[uint64](t, length, 0, 1),
		// Every delta is zero, so the whole bitmap reads 0xff: no nulls at all,
		// which contradicts the header.
		deltaNode[uint8](t, validityBytes-1, 0xff, sequenceNode[uint8](t, validityBytes-1, 0, 0)),
	)
	require.Less(t, len(stream), 200)

	err := assertRejectsWithin(t, func() error {
		_, err := LoadUnsigned[uint64](stream)
		return err
	})
	require.ErrorContains(t, err, "bitmap has 0 nulls, header says 1")
}

// TestSliceNullableOverDeltaSequenceValidity extends the read path's rule to
// the access path: anything charged per element must be O(1) per element. This
// bitmap agrees with its header, so the tree loads in about a millisecond, and
// slicing it then reached the validity child once per row — each read summing
// the whole delta prefix before it. 123 bytes bought 2m6s inside Slice, at
// 1/64th of the default length limit, and returned a correct answer.
func TestSliceNullableOverDeltaSequenceValidity(t *testing.T) {
	const length = 1 << 20
	const validityBytes = length / 8
	// Every validity byte decodes to 0x7f: seven valid rows and one null, which
	// is exactly what the header declares.
	const nulls = validityBytes
	var nullCount [nullableBodySize]byte
	binary.LittleEndian.PutUint64(nullCount[:], nulls)
	stream := codecNode(t, codecHeader{
		Version:  versionNumber,
		Type:     CodecTypeNullable,
		ElemType: PTypeUint64,
		Length:   length,
		NumBytes: nullableBodySize,
	},
		nullCount[:],
		sequenceNode[uint64](t, length, 0, 1),
		deltaNode[uint8](t, validityBytes-1, 0x7f, sequenceNode[uint8](t, validityBytes-1, 0, 0)),
	)
	require.Less(t, len(stream), 200)

	encoded, err := LoadUnsigned[uint64](stream)
	require.NoError(t, err)
	require.Equal(t, uint64(nulls), encoded.NullCount())

	var sliced EncodedArray[uint64]
	require.NoError(t, assertAnsweredWithin(t, func() error {
		var err error
		sliced, err = encoded.Slice(0, length)
		return err
	}))
	require.Equal(t, uint64(length), sliced.Length())
	require.Equal(t, uint64(nulls), sliced.NullCount())
	for _, offset := range []uint64{0, 7, 8, length - 1} {
		require.Equal(t, encoded.IsValid(offset), sliced.IsValid(offset), "validity at %d", offset)
	}
}

// TestSliceRunEndOverDeltaSequence is the same defect reached through run-end:
// its ValueAt binary-searches the ordinal child, whose own ValueAt is O(offset),
// and Slice called it once per row. 136 bytes and 16385 rows took 2.08s; the
// default length limit is 4096x that work.
func TestSliceRunEndOverDeltaSequence(t *testing.T) {
	const ends = 1 << 15
	const length = ends + 1
	stream := codecNode(t, codecHeader{
		Version:  versionNumber,
		Type:     CodecTypeRunEnd,
		ElemType: PTypeUint64,
		Length:   length,
	},
		sequenceNode[uint64](t, ends+1, 0, 1),
		deltaNode[uint64](t, ends-1, 1, sequenceNode[uint64](t, ends-1, 1, 0)),
	)
	require.Less(t, len(stream), 200)

	encoded, err := LoadUnsigned[uint64](stream)
	require.NoError(t, err)

	var sliced EncodedArray[uint64]
	require.NoError(t, assertAnsweredWithin(t, func() error {
		var err error
		sliced, err = encoded.Slice(0, length)
		return err
	}))
	want, err := Decompress(encoded)
	require.NoError(t, err)
	got, err := Decompress(sliced)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

// TestSliceHonorsCallerDecodedByteLimit pins which budget Slice decodes under.
// A codec that cannot slice structurally decodes the whole node to answer for
// any window, so it is the whole node that has to fit the limit the stream was
// loaded with — the window fitting proves nothing. Charged to the package
// default instead, a 72-byte stream answered Slice(0, 1) by allocating 8 MiB:
// 128x the limit its caller declared, and a paging loop's worth of it per page.
func TestSliceHonorsCallerDecodedByteLimit(t *testing.T) {
	const length = 1 << 20
	stream := deltaNode[uint64](t, length-1, 0, sequenceNode[uint64](t, length-1, 1, 0))
	require.Less(t, len(stream), 100)

	encoded, err := LoadUnsigned[uint64](stream, ReadOptions{MaxDecodedBytes: 1 << 16})
	require.NoError(t, err)

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err = encoded.Slice(0, 1)
	runtime.ReadMemStats(&after)
	require.ErrorIs(t, err, ErrMaterializationLimit)
	require.ErrorContains(t, err, "slice [0, 1) decodes all 1048576 rows of delta")
	require.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(1<<16), "refused, but only after decoding the array")

	// Same bytes, a limit that covers the whole node: the window is answered.
	encoded, err = LoadUnsigned[uint64](stream, ReadOptions{MaxDecodedBytes: 16 << 20})
	require.NoError(t, err)
	sliced, err := encoded.Slice(0, 1)
	require.NoError(t, err)
	require.Equal(t, uint64(1), sliced.Length())
	require.Equal(t, uint64(0), sliced.ValueAt(0))
}

func TestDecompressBitmapSparseOverDeltaSequence(t *testing.T) {
	// An all-ones bitmap makes every row a patch, and reaching each patch value
	// through the values child's ValueAt summed every delta before it: 16 KiB
	// bought a quadratic scatter that ran 9s and then succeeded. The child is
	// also a buffer DecompressInto holds live, so DecodedBytes must charge it
	// before the decode allocates it.
	const length = 1 << 17
	bitmap := bytes.Repeat([]byte{0xff}, length/8)
	stream := codecNode(t, codecHeader{
		Version:  versionNumber,
		Type:     CodecTypeSparse,
		ElemType: PTypeUint64,
		Flags:    flagSparseBitmap,
		Length:   length,
		NumBytes: uint64(len(bitmap)),
	}, bitmap,
		sequenceNode[uint64](t, 1, 0, 0),
		deltaNode[uint64](t, length-1, 0, sequenceNode[uint64](t, length-1, 1, 0)),
	)

	sparse, err := LoadUnsigned[uint64](stream)
	require.NoError(t, err)

	decoded, err := sparse.DecodedBytes()
	require.NoError(t, err)
	require.Greater(t, decoded, uint64(length*8), "patch values buffer is materialized but not accounted")

	require.NoError(t, assertAnsweredWithin(t, func() error {
		_, err := Decompress(sparse)
		return err
	}))
}

func TestLoadDecisionIgnoresTrailingBytes(t *testing.T) {
	// The work budget comes from ReadOptions alone. Seeding it from the bytes
	// left in the caller's buffer made this tree's acceptance depend on
	// whatever happened to follow it.
	const length = 1 << 16
	node := codecNode(t, codecHeader{
		Version:  versionNumber,
		Type:     CodecTypeDict,
		ElemType: PTypeUint64,
		Length:   length,
	},
		sequenceNode[uint64](t, 256, 1000, 1),
		sequenceNode[uint8](t, length, 0, 1),
	)

	exact, err := LoadUnsignedFromBuf[uint64](&array.BufReader{Buf: node})
	require.NoError(t, err)

	padded, err := LoadUnsignedFromBuf[uint64](&array.BufReader{Buf: append(node, make([]byte, 4096)...)})
	require.NoError(t, err)
	require.Equal(t, exact.Length(), padded.Length())
}

func TestLoadChargesValidationWorkToTheBudget(t *testing.T) {
	const length = 1 << 16
	node := codecNode(t, codecHeader{
		Version:  versionNumber,
		Type:     CodecTypeDict,
		ElemType: PTypeUint64,
		Length:   length,
	},
		sequenceNode[uint64](t, 256, 1000, 1),
		sequenceNode[uint8](t, length, 0, 1),
	)

	_, err := LoadUnsigned[uint64](node, ReadOptions{MaxWork: length})
	require.NoError(t, err)

	_, err = LoadUnsigned[uint64](node, ReadOptions{MaxWork: length - 1})
	require.ErrorIs(t, err, ErrWorkLimit)
}
