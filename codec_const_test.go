package btrblocks

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

func TestConstCodecUint64RoundTrip(t *testing.T) {
	data := makeConstantCorpus(1024, uint64(77))

	codec, err := NewConstIntegerCodec(array.NewPrimitivesUnsafe[uint64](data))
	if err != nil {
		t.Fatalf("NewConstIntegerCodec() returned error: %v", err)
	}

	assertCodecMetadata(t, codec, len(data), PTypeUint64, 0)
	assertCodecRoundTrip(t, codec, data)
}

func TestConstCodecErrorsAndFloatBitPatterns(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		if _, err := NewConstIntegerCodec(array.NewPrimitivesUnsafe[int64]([]int64{})); err != errDataEmpty {
			t.Fatalf("NewConstIntegerCodec() error = %v, want %v", err, errDataEmpty)
		}
	})

	t.Run("not constant", func(t *testing.T) {
		if _, err := NewConstStringCodec(array.NewStrings([]string{"a", "b"})); err != errValueNotConstant {
			t.Fatalf("NewConstStringCodec() error = %v, want %v", err, errValueNotConstant)
		}
	})

	t.Run("nan bitwise equality", func(t *testing.T) {
		value := math.Float64frombits(0x7ff8000000000001)
		data := makeConstantCorpus(16, value)

		codec, err := NewConstFloatCodec(array.NewPrimitivesUnsafe[float64](data))
		if err != nil {
			t.Fatalf("NewConstFloatCodec() returned error: %v", err)
		}

		assertCodecMetadata(t, codec, len(data), PTypeFloat64, 0)
		assertCodecRoundTrip(t, codec, data)
	})
}

func TestConstCodecLargeCorpus(t *testing.T) {
	data := makeConstantCorpus(largeCorpusSize, uint64(1<<32+9))

	codec, err := NewConstIntegerCodec(array.NewPrimitivesUnsafe[uint64](data))
	if err != nil {
		t.Fatalf("NewConstIntegerCodec() returned error: %v", err)
	}

	assertCodecMetadata(t, codec, len(data), PTypeUint64, 0)
	assertCodecRoundTrip(t, codec, data)
}

func FuzzConstCodecRoundTrip(f *testing.F) {
	f.Add(uint64(7), uint16(1))
	f.Add(uint64(42), uint16(128))
	f.Add(uint64(1<<32+1), uint16(2048))

	f.Fuzz(func(t *testing.T, value uint64, length uint16) {
		size := int(length%2048) + 1
		data := makeConstantCorpus(size, value)

		codec, err := NewConstIntegerCodec(array.NewPrimitivesUnsafe[uint64](data))
		if err != nil {
			t.Fatalf("NewConstIntegerCodec() returned error: %v", err)
		}

		assertCodecMetadata(t, codec, len(data), PTypeUint64, 0)
		assertCodecRoundTrip(t, codec, data)
	})
}

func BenchmarkConstCodecBuildLarge(b *testing.B) {
	data := makeConstantCorpus(largeCorpusSize, uint64(11))
	benchmarkBuildLoop(b, "const", func(values []uint64) (Codec[uint64], error) {
		return NewConstIntegerCodec(array.NewPrimitivesUnsafe[uint64](values))
	}, data)
}

func BenchmarkConstCodecValueAtLarge(b *testing.B) {
	data := makeConstantCorpus(largeCorpusSize, uint64(11))
	codec, err := NewConstIntegerCodec(array.NewPrimitivesUnsafe[uint64](data))
	if err != nil {
		b.Fatalf("NewConstIntegerCodec() returned error: %v", err)
	}
	benchmarkValueAtLoop(b, codec, len(data))
}

func TestReadConstCodecRejectsMalformedEmptyBody(t *testing.T) {
	body := array.NewPrimitivesUnsafe([]uint64{})

	var bodyBuf bytes.Buffer
	_, err := body.WriteTo(&bodyBuf)
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = Header{
		Version:    1,
		Kind:       CodecTypeConst,
		ElemType:   PTypeUint64,
		ChildCount: 0,
		Flags:      0,
		Length:     1,
		BodySize:   uint64(bodyBuf.Len()),
	}.WriteTo(&buf)
	require.NoError(t, err)
	buf.Write(bodyBuf.Bytes())

	_, err = readCodec[uint64](bytes.NewReader(buf.Bytes()))
	require.ErrorContains(t, err, "body length")
}

func TestReadConstCodecRejectsMismatchedOuterBodySize(t *testing.T) {
	codec, err := NewConstIntegerCodec(array.NewPrimitivesUnsafe([]uint64{7, 7, 7}))
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	data := append([]byte(nil), buf.Bytes()...)
	binary.LittleEndian.PutUint64(data[16:24], 1)

	_, err = readCodec[uint64](bytes.NewReader(data))
	require.ErrorContains(t, err, "const body size")
}
