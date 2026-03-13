package btrblocks

import (
	"bytes"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

func TestZigzagCodecInt64RoundTripAndWriteTo(t *testing.T) {
	data := []int64{-1 << 63, -17, -1, 0, 1, 17, 1<<63 - 1}

	codec, err := NewZigzagCodec(data, 1)
	require.NoError(t, err)

	assertCodecMetadata(t, codec, len(data), PTypeInt64, 1)
	assertCodecRoundTrip(t, codec, data)
	require.IsType(t, &RawCodec[uint64]{}, codec.data)

	var buf bytes.Buffer
	n, err := codec.WriteTo(&buf)
	require.NoError(t, err)
	require.Equal(t, int64(codec.BinarySize()), n)
	require.Equal(t, int(n), buf.Len())

	header, err := readHeader(&buf)
	require.NoError(t, err)
	require.Equal(t, CodecTypeZigzag, header.Kind)
	require.Equal(t, PTypeInt64, header.ElemType)
	require.Equal(t, uint64(len(data)), header.Length)
	require.Zero(t, header.BodySize)
	require.EqualValues(t, 1, header.ChildCount)

	childHeader, err := readHeader(&buf)
	require.NoError(t, err)
	require.Equal(t, CodecTypeRaw, childHeader.Kind)
	require.Equal(t, PTypeUint64, childHeader.ElemType)
	require.Equal(t, uint64(len(data)), childHeader.Length)
	require.Zero(t, childHeader.ChildCount)

	got, err := array.ReadPrimitives[uint64](&buf)
	require.NoError(t, err)
	require.Equal(t, uint64(len(data)), got.Length())

	want := zigzagEncodeSlice(data)
	for i, wantValue := range want {
		require.Equal(t, wantValue, got.ValueAt(uint64(i)))
	}
}
