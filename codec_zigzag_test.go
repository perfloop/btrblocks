package btrblocks

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

func TestZigzagCodecInt64RoundTripAndWriteTo(t *testing.T) {
	data := []int64{-1 << 63, -17, -1, 0, 1, 17, 1<<63 - 1}

	codec, err := NewZigzagCodec(array.NewPrimitivesUnsafe(data), 1)
	require.NoError(t, err)

	assertCodecMetadata(t, codec, len(data), PTypeInt64, 1)
	assertCodecRoundTrip(t, codec, data)

	var buf bytes.Buffer
	n, err := codec.WriteTo(&buf)
	require.NoError(t, err)
	require.Equal(t, int64(codec.BinarySize()), n)
	require.Equal(t, int(n), buf.Len())

	decoded, err := readCodec[int64](bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	require.Equal(t, uint64(len(data)), decoded.Length())
	require.Equal(t, PTypeInt64, decoded.PType())
	require.Len(t, decoded.Children(), 1)

	for i, value := range data {
		got, err := decoded.ValueAt(uint64(i))
		require.NoError(t, err)
		require.Equal(t, value, got)
	}
}

func TestNewZigzagCodecChoosesSmallestUnsignedChildWidth(t *testing.T) {
	tests := []struct {
		name     string
		data     []int64
		wantType PType
	}{
		{
			name:     "uint8",
			data:     []int64{-1, 0, 1, 63},
			wantType: PTypeUint8,
		},
		{
			name:     "uint16",
			data:     []int64{-129, 128},
			wantType: PTypeUint16,
		},
		{
			name:     "uint32",
			data:     []int64{-32769, 32768},
			wantType: PTypeUint32,
		},
		{
			name:     "uint64",
			data:     []int64{-2147483649, 2147483648},
			wantType: PTypeUint64,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codec, err := NewZigzagCodec(array.NewPrimitivesUnsafe(tt.data), 1)
			require.NoError(t, err)

			var buf bytes.Buffer
			_, err = codec.WriteTo(&buf)
			require.NoError(t, err)

			_, err = readHeader(&buf)
			require.NoError(t, err)

			childHeader, err := readHeader(&buf)
			require.NoError(t, err)
			require.Equal(t, tt.wantType, childHeader.ElemType)
		})
	}
}

func TestReadZigzagCodecRejectsNonUnsignedChild(t *testing.T) {
	tests := []struct {
		name       string
		writeChild func(*bytes.Buffer) (int64, error)
	}{
		{
			name: "signed child",
			writeChild: func(buf *bytes.Buffer) (int64, error) {
				return NewRawCodec(array.NewPrimitivesUnsafe([]int8{1})).WriteTo(buf)
			},
		},
		{
			name: "float child",
			writeChild: func(buf *bytes.Buffer) (int64, error) {
				return NewRawCodec(array.NewPrimitivesUnsafe([]float32{1})).WriteTo(buf)
			},
		},
		{
			name: "string child",
			writeChild: func(buf *bytes.Buffer) (int64, error) {
				return NewRawCodec(array.NewStrings([]string{"x"})).WriteTo(buf)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			_, err := Header{
				Version:    1,
				Kind:       CodecTypeZigzag,
				ElemType:   PTypeInt64,
				ChildCount: 1,
				Flags:      0,
				Length:     1,
				BodySize:   0,
			}.WriteTo(&buf)
			require.NoError(t, err)

			_, err = tt.writeChild(&buf)
			require.NoError(t, err)

			_, err = readCodec[int64](bytes.NewReader(buf.Bytes()))
			require.ErrorContains(t, err, "zigzag child element type")
		})
	}
}

func TestReadZigzagCodecZeroLengthRoundTrip(t *testing.T) {
	codec, err := NewZigzagCodec(array.NewPrimitivesUnsafe([]int64{}), 1)
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	decoded, err := readCodec[int64](bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	assertCodecMetadata(t, decoded, 0, PTypeInt64, 1)
}

func TestReadZigzagCodecRejectsMismatchedOuterMetadata(t *testing.T) {
	codec, err := NewZigzagCodec(array.NewPrimitivesUnsafe([]int64{-1, 0, 1}), 1)
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	tests := []struct {
		name   string
		mutate func([]byte)
		want   string
	}{
		{
			name: "body size",
			mutate: func(data []byte) {
				binary.LittleEndian.PutUint64(data[16:24], 1)
			},
			want: "zigzag body size",
		},
		{
			name: "length",
			mutate: func(data []byte) {
				binary.LittleEndian.PutUint64(data[8:16], 4)
			},
			want: "zigzag length",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := append([]byte(nil), buf.Bytes()...)
			tt.mutate(data)

			_, err := readCodec[int64](bytes.NewReader(data))
			require.ErrorContains(t, err, tt.want)
		})
	}
}
