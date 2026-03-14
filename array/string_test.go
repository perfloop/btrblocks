package array

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewStringsChoosesOffsetWidth(t *testing.T) {
	t.Run("uint8", func(t *testing.T) {
		arr := NewStrings([]string{strings.Repeat("a", math.MaxUint8)})
		require.IsType(t, &Strings[uint8]{}, arr)
	})
	t.Run("uint16", func(t *testing.T) {
		arr := NewStrings([]string{strings.Repeat("a", math.MaxUint8+1)})
		require.IsType(t, &Strings[uint16]{}, arr)
	})
	t.Run("uint32", func(t *testing.T) {
		arr := NewStrings([]string{strings.Repeat("a", math.MaxUint16+1)})
		require.IsType(t, &Strings[uint32]{}, arr)
	})
}

func TestStringsMetadataAndHeader(t *testing.T) {
	arr := newStringsWithOffsets[uint16]([]string{"go", "", "lang"}, 6, 2)

	require.EqualValues(t, 3, arr.Length())
	require.Equal(t, PTypeString, arr.PType())
	require.EqualValues(t, 38, arr.BinarySize())
	require.Equal(t, "", arr.ValueAt(1))
	require.Equal(t, "lang", arr.ValueAt(2))
	require.Equal(t, Header{Version: 1, PType: PTypeString, Length: 3, BodySize: 18}, arr.header())
}

func TestStringsWriteToIncludesHeaderOffsetsAndBuffer(t *testing.T) {
	arr := newStringsWithOffsets[uint16]([]string{"go", "lang"}, 6, 2)

	var buf bytes.Buffer
	n, err := arr.WriteTo(&buf)
	require.NoError(t, err)

	wantBody := make([]byte, 0, 16)
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], 6)
	wantBody = append(wantBody, lenBuf[:]...)
	wantBody = binary.LittleEndian.AppendUint16(wantBody, 0)
	wantBody = binary.LittleEndian.AppendUint16(wantBody, 2)
	wantBody = binary.LittleEndian.AppendUint16(wantBody, 6)
	wantBody = append(wantBody, []byte("golang")...)

	require.EqualValues(t, headerSize+len(wantBody), n)

	got := buf.Bytes()
	assertHeaderBytes(t, got[:headerSize], Header{
		Version:  1,
		PType:    PTypeString,
		Length:   2,
		BodySize: uint64(len(wantBody)),
	})
	require.Equal(t, wantBody, got[headerSize:])
}

func TestStringsLargeCorpus(t *testing.T) {
	pool := []string{"", "a", "bb", "ccc"}
	values := make([]string, largeCorpusSize)
	total := 0
	for i := range values {
		values[i] = pool[i%len(pool)]
		total += len(values[i])
	}

	arr := NewStrings(values)
	stringsArr, ok := arr.(*Strings[uint32])
	require.True(t, ok, "NewStrings() type = %T, want *Strings[uint32]", arr)

	require.EqualValues(t, largeCorpusSize, arr.Length())
	wantBinarySize := uint64(headerSize) + 4 + uint64(total) + uint64(largeCorpusSize+1)*4
	require.Equal(t, wantBinarySize, arr.BinarySize())

	for _, idx := range []uint64{0, 1, largeCorpusSize / 2, largeCorpusSize - 1} {
		require.Equal(t, values[idx], arr.ValueAt(idx), "ValueAt(%d)", idx)
	}

	require.Equal(t, Header{Version: 1, PType: PTypeString, Length: largeCorpusSize, BodySize: 4 + uint64(total) + uint64(largeCorpusSize+1)*4}, stringsArr.header())

	n, err := arr.WriteTo(io.Discard)
	require.NoError(t, err)
	require.EqualValues(t, wantBinarySize, n)
}

func TestReadStrings(t *testing.T) {
	values := []string{"go", "", "lang"}
	arr := NewStrings(values)
	var buf bytes.Buffer
	_, err := arr.WriteTo(&buf)
	require.NoError(t, err)
	got, err := ReadStrings(&buf)
	require.NoError(t, err)
	require.Equal(t, arr.Length(), got.Length())
	for i := uint64(0); i < got.Length(); i++ {
		require.Equal(t, values[i], got.ValueAt(i), "ValueAt(%d)", i)
	}
}

func TestReadStringsRejectsInvalidOffsets(t *testing.T) {
	arr := NewStrings([]string{"go", "lang"})

	var buf bytes.Buffer
	_, err := arr.WriteTo(&buf)
	require.NoError(t, err)

	data := buf.Bytes()
	data[headerSize+4+2] = 1

	_, err = ReadStrings(bytes.NewReader(data))
	require.ErrorContains(t, err, "offsets")
}

func BenchmarkWriteStrings(b *testing.B) {
	values := make([]string, 1000)
	for i := range values {
		values[i] = "hello"
	}
	arr := NewStrings(values)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = arr.WriteTo(io.Discard)
	}
}

func BenchmarkReadStrings(b *testing.B) {
	values := make([]string, 1000)
	for i := range values {
		values[i] = "hello"
	}
	arr := NewStrings(values)
	var buf bytes.Buffer
	_, _ = arr.WriteTo(&buf)
	data := buf.Bytes()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ReadStrings(bytes.NewReader(data))
	}
}

func BenchmarkValueAtStrings(b *testing.B) {
	values := make([]string, 1000)
	for i := range values {
		values[i] = "hello"
	}
	arr := NewStrings(values)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = arr.ValueAt(uint64(i % 1000))
	}
}

func FuzzReadStrings(f *testing.F) {
	// Seed with valid encoded string array so corpus has at least one valid input.
	arr := NewStrings([]string{"a", "bb", "ccc"})
	var buf bytes.Buffer
	_, _ = arr.WriteTo(&buf)
	f.Add(buf.Bytes())
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ReadStrings(bytes.NewReader(data))
		// Must not panic; error is acceptable for invalid/corrupt input.
	})
}
