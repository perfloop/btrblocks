package array

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func TestNewStringsChoosesOffsetWidth(t *testing.T) {
	t.Run("uint8", func(t *testing.T) {
		arr := mustStrings(t, []string{strings.Repeat("a", math.MaxUint8)})
		if _, ok := arr.(*Strings[uint8]); !ok {
			t.Fatalf("arr = %T, want *Strings[uint8]", arr)
		}
	})
	t.Run("uint16", func(t *testing.T) {
		arr := mustStrings(t, []string{strings.Repeat("a", math.MaxUint8+1)})
		if _, ok := arr.(*Strings[uint16]); !ok {
			t.Fatalf("arr = %T, want *Strings[uint16]", arr)
		}
	})
	t.Run("uint32", func(t *testing.T) {
		arr := mustStrings(t, []string{strings.Repeat("a", math.MaxUint16+1)})
		if _, ok := arr.(*Strings[uint32]); !ok {
			t.Fatalf("arr = %T, want *Strings[uint32]", arr)
		}
	})
}

func TestNewStringsWithNulls(t *testing.T) {
	arr, err := NewStringsWithNulls([]string{"go", "", "lang"}, []bool{false, true, false})
	if err != nil {
		t.Fatalf("NewStringsWithNulls: %v", err)
	}
	if got := arr.NullCount(); got != 1 {
		t.Fatalf("NullCount = %d, want 1", got)
	}
	if !arr.IsValid(0) {
		t.Fatal("IsValid(0) = false, want true")
	}
	if arr.IsValid(1) {
		t.Fatal("IsValid(1) = true, want false")
	}

	allValid, err := NewStringsWithNulls([]string{"go", "lang"}, []bool{})
	if err != nil {
		t.Fatalf("NewStringsWithNulls with empty mask: %v", err)
	}
	if got := allValid.NullCount(); got != 0 {
		t.Fatalf("empty-mask NullCount = %d, want 0", got)
	}
	if !allValid.IsValid(0) || !allValid.IsValid(1) {
		t.Fatal("empty null mask produced an invalid row")
	}

	_, err = NewStringsWithNulls([]string{"go", "lang"}, []bool{false})
	if err == nil {
		t.Fatal("NewStringsWithNulls accepted a mismatched null mask")
	}
}

func TestTotalStringBytesRejectsFormatLimit(t *testing.T) {
	require.NoError(t, validateStringDataSize(math.MaxUint32))

	err := validateStringDataSize(math.MaxUint32 + 1)
	require.ErrorContains(t, err, "format limit")
}

func TestStringsMetadataAndHeader(t *testing.T) {
	arr := newStringsWithOffsets[uint16]([]string{"go", "", "lang"}, 6, AllValid(3))

	require.Equal(t, uint64(3), arr.Length())
	require.Equal(t, PTypeString, arr.PType())
	require.Equal(t, uint64(38), arr.BinarySize())
	require.Equal(t, "", arr.ValueAt(1))
	require.Equal(t, "lang", arr.ValueAt(2))
	require.Equal(t, Header{Version: FormatVersion, PType: PTypeString, Length: 3, NumBytes: 18}, arr.header())
}

func TestStringsValueAtUsesBackingBuffer(t *testing.T) {
	arr := newStringsWithOffsets[uint16]([]string{"go", "", "lang"}, 6, AllValid(3))
	got := arr.ValueAt(2)

	require.Equal(t, "lang", got)
	require.Equal(t, unsafe.StringData(got), unsafe.SliceData(arr.buf[2:6]))
}

func TestStringsWriteToIncludesHeaderOffsetsAndBuffer(t *testing.T) {
	arr := newStringsWithOffsets[uint16]([]string{"go", "lang"}, 6, AllValid(2))

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

	require.Equal(t, int64(headerSize+len(wantBody)), n)

	got := buf.Bytes()
	assertHeaderBytes(t, got[:headerSize], Header{
		Version:  FormatVersion,
		PType:    PTypeString,
		Length:   2,
		NumBytes: uint64(len(wantBody)),
	})
	require.Equal(t, wantBody, got[headerSize:])
}

func TestStringsSlice(t *testing.T) {
	arr := newStringsWithOffsets[uint16]([]string{"go", "", "lang"}, 6, AllValid(3))

	slicedAny, err := arr.Slice(1, 3)
	require.NoError(t, err)
	sliced, ok := slicedAny.(*Strings[uint16])
	require.True(t, ok)
	require.Equal(t, uint64(2), sliced.Length())
	require.Equal(t, "", sliced.ValueAt(0))
	require.Equal(t, "lang", sliced.ValueAt(1))
	require.Equal(t, unsafe.SliceData(arr.buf[2:6]), unsafe.SliceData(sliced.buf))
}

func TestStringsLargeCorpus(t *testing.T) {
	pool := []string{"", "a", "bb", "ccc"}
	values := make([]string, largeCorpusSize)
	total := 0
	for i := range values {
		values[i] = pool[i%len(pool)]
		total += len(values[i])
	}

	arr := mustStrings(t, values)
	stringsArr, ok := arr.(*Strings[uint32])
	require.True(t, ok, "mustStrings(t, ) type = %T, want *Strings[uint32]", arr)

	require.Equal(t, uint64(largeCorpusSize), arr.Length())
	wantBinarySize := uint64(headerSize) + 4 + uint64(total) + uint64(largeCorpusSize+1)*4
	require.Equal(t, wantBinarySize, arr.BinarySize())

	for _, idx := range []uint64{0, 1, largeCorpusSize / 2, largeCorpusSize - 1} {
		require.Equal(t, values[idx], arr.ValueAt(idx), "ValueAt(%d)", idx)
	}

	require.Equal(t, Header{Version: FormatVersion, PType: PTypeString, Length: largeCorpusSize, NumBytes: 4 + uint64(total) + uint64(largeCorpusSize+1)*4}, stringsArr.header())

	n, err := arr.WriteTo(io.Discard)
	require.NoError(t, err)
	require.Equal(t, int64(wantBinarySize), n)
}

func TestReadStrings(t *testing.T) {
	values := []string{"go", "", "lang"}
	arr := mustStrings(t, values)
	var buf bytes.Buffer
	_, err := arr.WriteTo(&buf)
	require.NoError(t, err)
	got, err := readStringsFromBytes(buf.Bytes())
	require.NoError(t, err)
	require.Equal(t, arr.Length(), got.Length())
	for i := range got.Length() {
		require.Equal(t, values[i], got.ValueAt(i), "ValueAt(%d)", i)
	}
}

func TestReadStringsRejectsInvalidOffsets(t *testing.T) {
	arr := mustStrings(t, []string{"go", "lang"})

	var buf bytes.Buffer
	_, err := arr.WriteTo(&buf)
	require.NoError(t, err)

	data := buf.Bytes()
	data[headerSize+4+2] = 1

	_, err = readStringsFromBytes(data)
	require.ErrorContains(t, err, "offsets")
}

func TestReadStringsRejectsUnsupportedHeader(t *testing.T) {
	tests := []struct {
		name   string
		header Header
		want   string
	}{
		{
			name:   "version",
			header: Header{Version: 3, PType: PTypeString, Length: 0, NumBytes: 4},
			want:   "version",
		},
		{
			name:   "flags",
			header: Header{Version: FormatVersion, PType: PTypeString, Flags: 2, Length: 0, NumBytes: 4},
			want:   "flags",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			_, err := tt.header.WriteTo(&buf)
			require.NoError(t, err)

			_, err = readStringsFromBytes(buf.Bytes())
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestReadStringsRejectsInvalidBodySize(t *testing.T) {
	t.Run("too small for offsets", func(t *testing.T) {
		var buf bytes.Buffer
		_, err := Header{
			Version:  FormatVersion,
			PType:    PTypeString,
			Length:   1,
			NumBytes: 4,
		}.WriteTo(&buf)
		require.NoError(t, err)

		_, err = readStringsFromBytes(buf.Bytes())
		require.ErrorContains(t, err, "string body")
	})

	t.Run("buffer exceeds declared body", func(t *testing.T) {
		var buf bytes.Buffer
		_, err := Header{
			Version:  FormatVersion,
			PType:    PTypeString,
			Length:   0,
			NumBytes: 5,
		}.WriteTo(&buf)
		require.NoError(t, err)
		require.NoError(t, binary.Write(&buf, binary.LittleEndian, uint32(2)))

		_, err = readStringsFromBytes(buf.Bytes())
		require.ErrorContains(t, err, "string body")
	})
}

func TestStringsWriteToShortWrite(t *testing.T) {
	arr := mustStrings(t, []string{"go", "lang"})
	writer := &shortWriter{remaining: int(arr.BinarySize()) - 1}

	n, err := arr.WriteTo(writer)
	require.ErrorIs(t, err, io.ErrShortWrite)
	require.Equal(t, int64(arr.BinarySize()-1), n)
}

func BenchmarkWriteStrings(b *testing.B) {
	values := make([]string, 1000)
	for i := range values {
		values[i] = "hello"
	}
	arr := mustStrings(b, values)
	b.ResetTimer()
	for range b.N {
		_, _ = arr.WriteTo(io.Discard)
	}
}

func BenchmarkReadStrings(b *testing.B) {
	values := make([]string, 1000)
	for i := range values {
		values[i] = "hello"
	}
	arr := mustStrings(b, values)
	var buf bytes.Buffer
	_, _ = arr.WriteTo(&buf)
	data := buf.Bytes()
	b.ResetTimer()
	for range b.N {
		_, _ = readStringsFromBytes(data)
	}
}

func BenchmarkValueAtStrings(b *testing.B) {
	values := make([]string, 1000)
	for i := range values {
		values[i] = "hello"
	}
	arr := mustStrings(b, values)
	b.ResetTimer()
	for i := range b.N {
		_ = arr.ValueAt(uint64(i % 1000))
	}
}

func FuzzReadStrings(f *testing.F) {
	// Seed with valid encoded string array so corpus has at least one valid input.
	arr := mustStrings(f, []string{"a", "bb", "ccc"})
	var buf bytes.Buffer
	_, _ = arr.WriteTo(&buf)
	f.Add(buf.Bytes())
	f.Fuzz(func(_ *testing.T, data []byte) {
		opts := ReadOptions{MaxLength: 1 << 20, MaxBytes: 1 << 24}
		_, _ = readStringsFromBytes(data, opts)
		// Must not panic; error is acceptable for invalid/corrupt input.
	})
}
