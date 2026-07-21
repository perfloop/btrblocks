package array

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

// shortWriter simulates partial writes when testing header serialization.
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
		NumBytes: 0x1112131415161718,
	}

	var buf bytes.Buffer
	n, err := header.WriteTo(&buf)
	require.NoError(t, err)
	require.Equal(t, int64(headerSize), n)

	got := buf.Bytes()
	require.Equal(t, headerSize, len(got))
	require.Equal(t, header.Version, got[0])
	require.Equal(t, header.PType, PType(got[1]))
	require.Equal(t, header.Flags, binary.LittleEndian.Uint16(got[2:4]))
	require.Equal(t, header.Length, binary.LittleEndian.Uint64(got[4:12]))
	require.Equal(t, header.NumBytes, binary.LittleEndian.Uint64(got[12:20]))
}

func BenchmarkHeaderWriteTo(b *testing.B) {
	h := Header{Version: FormatVersion, PType: PTypeUint32, Length: 1000, NumBytes: 4000}
	var buf bytes.Buffer
	b.ResetTimer()
	for range b.N {
		buf.Reset()
		_, _ = h.WriteTo(&buf)
	}
}

func TestHeaderWriteToShortWrite(t *testing.T) {
	header := Header{Version: FormatVersion, PType: PTypeUint32, Length: 9, NumBytes: 32}
	writer := &shortWriter{remaining: headerSize - 1}

	n, err := header.WriteTo(writer)
	require.ErrorIs(t, err, io.ErrShortWrite)
	require.Equal(t, int64(headerSize-1), n)
}

func TestReadOptionsRejectsOversizedHeader(t *testing.T) {
	h := Header{Version: FormatVersion, PType: PTypeInt32, Length: 1_000_000, NumBytes: 4_000_000}
	require.NoError(t, validateHeader(h, ReadOptions{}))
	require.Error(t, validateHeader(h, ReadOptions{MaxLength: 100_000}))
	require.Error(t, validateHeader(h, ReadOptions{MaxBytes: 1_000_000}))
	require.NoError(t, validateHeader(h, ReadOptions{MaxLength: 2_000_000, MaxBytes: 5_000_000}))
}

func TestReadPrimitivesWithMaxLength(t *testing.T) {
	arr := NewPrimitives([]int32{1, 2, 3, 4, 5})
	var buf bytes.Buffer
	_, err := arr.WriteTo(&buf)
	require.NoError(t, err)

	// Without limits: succeeds.
	_, err = readPrimitiveFromBytes[int32](buf.Bytes())
	require.NoError(t, err)

	// With tight limit: rejected before allocation.
	_, err = readPrimitiveFromBytes[int32](buf.Bytes(), ReadOptions{MaxLength: 3})
	require.Error(t, err)
	require.ErrorContains(t, err, "exceeds limit")
}

func FuzzReadHeader(f *testing.F) {
	// Seed with valid header bytes so the fuzz corpus has at least one valid input.
	valid := make([]byte, headerSize)
	valid[0] = FormatVersion
	valid[1] = byte(PTypeInt32)
	binary.LittleEndian.PutUint64(valid[4:12], 100)
	binary.LittleEndian.PutUint64(valid[12:20], 400)
	f.Add(valid)
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = readHeaderFromBuf(&BufReader{Buf: data})
		// Must not panic; error is acceptable for invalid/corrupt input.
	})
}
