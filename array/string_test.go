package array

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"strings"
	"testing"
)

func TestNewStringsChoosesOffsetWidth(t *testing.T) {
	t.Run("uint8", func(t *testing.T) {
		arr := NewStrings([]string{strings.Repeat("a", math.MaxUint8)})
		if _, ok := arr.(*Strings[uint8]); !ok {
			t.Fatalf("NewStrings() type = %T, want *Strings[uint8]", arr)
		}
	})
	t.Run("uint16", func(t *testing.T) {
		arr := NewStrings([]string{strings.Repeat("a", math.MaxUint8+1)})
		if _, ok := arr.(*Strings[uint16]); !ok {
			t.Fatalf("NewStrings() type = %T, want *Strings[uint16]", arr)
		}
	})
	t.Run("uint32", func(t *testing.T) {
		arr := NewStrings([]string{strings.Repeat("a", math.MaxUint16+1)})
		if _, ok := arr.(*Strings[uint32]); !ok {
			t.Fatalf("NewStrings() type = %T, want *Strings[uint32]", arr)
		}
	})
}

func TestStringsMetadataAndHeader(t *testing.T) {
	arr := newStringsWithOffsets[uint16]([]string{"go", "", "lang"}, 6, 2)

	if got := arr.Length(); got != 3 {
		t.Fatalf("Length() = %d, want 3", got)
	}
	if got := arr.PType(); got != PTypeString {
		t.Fatalf("PType() = %v, want %v", got, PTypeString)
	}
	if got := arr.BinarySize(); got != 14 {
		t.Fatalf("BinarySize() = %d, want 14", got)
	}
	if got := arr.ValueAt(1); got != "" {
		t.Fatalf("ValueAt(1) = %q, want empty string", got)
	}
	if got := arr.ValueAt(2); got != "lang" {
		t.Fatalf("ValueAt(2) = %q, want %q", got, "lang")
	}
	if got := arr.header(); got != (Header{Version: 1, PType: PTypeString, Length: 3, BodySize: 18}) {
		t.Fatalf("header() = %+v, want %+v", got, Header{Version: 1, PType: PTypeString, Length: 3, BodySize: 18})
	}
}

func TestStringsWriteToIncludesHeaderOffsetsAndBuffer(t *testing.T) {
	arr := newStringsWithOffsets[uint16]([]string{"go", "lang"}, 6, 2)

	var buf bytes.Buffer
	n, err := arr.WriteTo(&buf)
	if err != nil {
		t.Fatalf("WriteTo() error = %v", err)
	}

	wantBody := make([]byte, 0, 16)
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], 6)
	wantBody = append(wantBody, lenBuf[:]...)
	wantBody = binary.LittleEndian.AppendUint16(wantBody, 0)
	wantBody = binary.LittleEndian.AppendUint16(wantBody, 2)
	wantBody = binary.LittleEndian.AppendUint16(wantBody, 6)
	wantBody = append(wantBody, []byte("golang")...)

	if want := int64(headerSize + len(wantBody)); n != want {
		t.Fatalf("WriteTo() bytes = %d, want %d", n, want)
	}

	got := buf.Bytes()
	assertHeaderBytes(t, got[:headerSize], Header{
		Version:  1,
		PType:    PTypeString,
		Length:   2,
		BodySize: uint64(len(wantBody)),
	})
	if !bytes.Equal(got[headerSize:], wantBody) {
		t.Fatalf("body bytes = %v, want %v", got[headerSize:], wantBody)
	}
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
	if !ok {
		t.Fatalf("NewStrings() type = %T, want *Strings[uint32]", arr)
	}

	if got := arr.Length(); got != largeCorpusSize {
		t.Fatalf("Length() = %d, want %d", got, largeCorpusSize)
	}
	wantBinarySize := uint64(total) + uint64(largeCorpusSize+1)*4
	if got := arr.BinarySize(); got != wantBinarySize {
		t.Fatalf("BinarySize() = %d, want %d", got, wantBinarySize)
	}

	for _, idx := range []uint64{0, 1, largeCorpusSize / 2, largeCorpusSize - 1} {
		if got := arr.ValueAt(idx); got != values[idx] {
			t.Fatalf("ValueAt(%d) = %q, want %q", idx, got, values[idx])
		}
	}

	if got := stringsArr.header(); got != (Header{Version: 1, PType: PTypeString, Length: largeCorpusSize, BodySize: 4 + wantBinarySize}) {
		t.Fatalf("header() = %+v, want %+v", got, Header{Version: 1, PType: PTypeString, Length: largeCorpusSize, BodySize: 4 + wantBinarySize})
	}

	n, err := arr.WriteTo(io.Discard)
	if err != nil {
		t.Fatalf("WriteTo() error = %v", err)
	}
	if want := int64(headerSize) + int64(4+wantBinarySize); n != want {
		t.Fatalf("WriteTo() bytes = %d, want %d", n, want)
	}
}
