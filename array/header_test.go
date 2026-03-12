package array

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

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
