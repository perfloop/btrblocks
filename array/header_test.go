package array

import (
	"bytes"
	"encoding/binary"
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

func TestHeaderWriteTo(t *testing.T) {
	header := Header{
		Version:  2,
		PType:    PTypeUint32,
		Flags:    0x1122,
		Length:   0x0102030405060708,
		BodySize: 0x1112131415161718,
	}

	var buf bytes.Buffer
	n, err := header.WriteTo(&buf)
	require.NoError(t, err)
	require.EqualValues(t, headerSize, n)

	got := buf.Bytes()
	require.Len(t, got, headerSize)
	require.Equal(t, header.Version, got[0])
	require.Equal(t, header.PType, PType(got[1]))
	require.Equal(t, header.Flags, binary.LittleEndian.Uint16(got[2:4]))
	require.Equal(t, header.Length, binary.LittleEndian.Uint64(got[4:12]))
	require.Equal(t, header.BodySize, binary.LittleEndian.Uint64(got[12:20]))
}

func BenchmarkHeaderWriteTo(b *testing.B) {
	h := Header{Version: 1, PType: PTypeUint32, Length: 1000, BodySize: 4000}
	var buf bytes.Buffer
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		_, _ = h.WriteTo(&buf)
	}
}

func TestHeaderWriteToShortWrite(t *testing.T) {
	header := Header{Version: 1, PType: PTypeUint32, Length: 9, BodySize: 32}
	writer := &shortWriter{remaining: headerSize - 1}

	n, err := header.WriteTo(writer)
	require.ErrorIs(t, err, io.ErrShortWrite)
	require.EqualValues(t, headerSize-1, n)
}

func FuzzReadHeader(f *testing.F) {
	// Seed with valid header bytes so the fuzz corpus has at least one valid input.
	valid := make([]byte, headerSize)
	valid[0] = 1
	valid[1] = byte(PTypeInt32)
	binary.LittleEndian.PutUint64(valid[4:12], 100)
	binary.LittleEndian.PutUint64(valid[12:20], 400)
	f.Add(valid)
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = readHeader(bytes.NewReader(data))
		// Must not panic; error is acceptable for invalid/corrupt input.
	})
}
