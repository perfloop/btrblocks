package btrblocks

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func mustEncodeCodec(t *testing.T, codec io.WriterTo) []byte {
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
	_, err := header{
		Version:  versionNumber,
		Kind:     CodeType(8),
		ElemType: PTypeUint32,
		Length:   1,
	}.WriteTo(&buf)
	require.NoError(t, err)

	_, err = Read[uint32](&buf)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown kind = 8")
}

func TestReadRunEndRejectsZeroFirstEnd(t *testing.T) {
	data := mustEncodeCodec(t, &runEndCodec[uint32, uint8]{
		length: 3,
		runs:   newRawCodec(buildArray([]uint32{1, 2})),
		ends:   newRawCodec(buildArray([]uint8{0})),
	})

	_, err := Read[uint32](bytes.NewReader(data))
	require.Error(t, err)
	require.Contains(t, err.Error(), "runend first end = 0, want > 0")
}

func TestReadRunEndRejectsTerminalEndAtLength(t *testing.T) {
	data := mustEncodeCodec(t, &runEndCodec[uint32, uint8]{
		length: 3,
		runs:   newRawCodec(buildArray([]uint32{1, 2})),
		ends:   newRawCodec(buildArray([]uint8{3})),
	})

	_, err := Read[uint32](bytes.NewReader(data))
	require.Error(t, err)
	require.Contains(t, err.Error(), "runend last end = 3, want < 3")
}

func TestReadALP64RejectsEmptyPatches(t *testing.T) {
	data := mustEncodeCodec(t, &alpCodec64{
		length:    3,
		expE:      0,
		expF:      0,
		encoded:   newRawCodec(buildArray([]int64{1, 2, 3})),
		patchIdxC: newRawCodec(buildArray([]uint32{})),
		patchValC: newRawCodec(buildArray([]float64{})),
	})

	_, err := Read[float64](bytes.NewReader(data))
	require.Error(t, err)
	require.Contains(t, err.Error(), "ALP patches length = 0")
}

func TestReadALP32RejectsOutOfRangePatchIndex(t *testing.T) {
	data := mustEncodeCodec(t, &alpCodec32{
		length:    3,
		expE:      0,
		expF:      0,
		encoded:   newRawCodec(buildArray([]int32{1, 2, 3})),
		patchIdxC: newRawCodec(buildArray([]uint32{3})),
		patchValC: newRawCodec(buildArray([]float32{1.25})),
	})

	_, err := Read[float32](bytes.NewReader(data))
	require.Error(t, err)
	require.Contains(t, err.Error(), "ALP patch index = 3, want < 3")
}

func TestReadALP64KeepsPatchesEncodedUntilDecode(t *testing.T) {
	data := mustEncodeCodec(t, &alpCodec64{
		length:    4,
		expE:      0,
		expF:      0,
		encoded:   newRawCodec(buildArray([]int64{1, 2, 3, 4})),
		patchIdxC: newRawCodec(buildArray([]uint32{1, 3})),
		patchValC: newRawCodec(buildArray([]float64{20.5, 40.5})),
	})

	readCodec, err := Read[float64](bytes.NewReader(data))
	require.NoError(t, err)

	codec := readCodec.(*alpCodec64)
	require.NotNil(t, codec.patchIdxC)
	require.NotNil(t, codec.patchValC)

	require.Equal(t, 1.0, codec.ValueAt(0))
	require.Equal(t, 20.5, codec.ValueAt(1))
	require.Equal(t, 3.0, codec.ValueAt(2))
	require.Equal(t, 40.5, codec.ValueAt(3))

	decoded := make([]float64, 4)
	require.NoError(t, codec.Decode(decoded))
	require.Equal(t, []float64{1.0, 20.5, 3.0, 40.5}, decoded)
}

func TestReadALP32KeepsPatchesEncodedUntilDecode(t *testing.T) {
	data := mustEncodeCodec(t, &alpCodec32{
		length:    3,
		expE:      0,
		expF:      0,
		encoded:   newRawCodec(buildArray([]int32{1, 2, 3})),
		patchIdxC: newRawCodec(buildArray([]uint32{0, 2})),
		patchValC: newRawCodec(buildArray([]float32{9.25, 7.5})),
	})

	readCodec, err := Read[float32](bytes.NewReader(data))
	require.NoError(t, err)

	codec := readCodec.(*alpCodec32)
	require.NotNil(t, codec.patchIdxC)
	require.NotNil(t, codec.patchValC)

	require.Equal(t, float32(9.25), codec.ValueAt(0))
	require.Equal(t, float32(2.0), codec.ValueAt(1))
	require.Equal(t, float32(7.5), codec.ValueAt(2))

	decoded := make([]float32, 3)
	require.NoError(t, codec.Decode(decoded))
	require.Equal(t, []float32{9.25, 2.0, 7.5}, decoded)
}
