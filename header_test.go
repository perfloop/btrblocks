package btrblocks

import (
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

type shortWriter struct {
	remaining int
}

func (w *shortWriter) Write(p []byte) (int, error) {
	if w.remaining <= 0 {
		return 0, nil
	}
	if len(p) > w.remaining {
		n := w.remaining
		w.remaining = 0
		return n, nil
	}
	w.remaining -= len(p)
	return len(p), nil
}

type partialErrorWriter struct {
	calls    int
	failCall int
	err      error
}

func (w *partialErrorWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls == w.failCall {
		if len(p) == 0 {
			return 0, w.err
		}
		return 1, w.err
	}
	return len(p), nil
}

func TestHeaderWriteToShortWrite(t *testing.T) {
	header := Header{
		Version:    1,
		Kind:       CodecTypeRaw,
		ElemType:   PTypeUint64,
		ChildCount: 0,
		Flags:      0,
		Length:     9,
		BodySize:   32,
	}

	writer := &shortWriter{remaining: headerSize - 1}
	n, err := header.WriteTo(writer)
	require.ErrorIs(t, err, io.ErrShortWrite)
	require.EqualValues(t, headerSize-1, n)
}

func TestBitpackingCodecWriteToShortWrite(t *testing.T) {
	codec := NewBitpackingCodec([]uint8{0, 1, 2, 3, 4, 5, 6, 7})

	writer := &shortWriter{remaining: headerSize + 1 + len(codec.buf) - 1}
	n, err := codec.WriteTo(writer)
	require.ErrorIs(t, err, io.ErrShortWrite)
	require.EqualValues(t, headerSize+1+len(codec.buf)-1, n)
}

func TestBitpackingCodecWriteToCountsPartialErrorOnWidthByte(t *testing.T) {
	codec := NewBitpackingCodec([]uint8{0, 1, 2, 3, 4, 5, 6, 7})
	wantErr := errors.New("write failed")
	writer := &partialErrorWriter{failCall: 2, err: wantErr}

	n, err := codec.WriteTo(writer)
	require.ErrorIs(t, err, wantErr)
	require.EqualValues(t, headerSize+1, n)
}
