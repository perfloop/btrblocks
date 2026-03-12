package array

import (
	"bytes"
	"encoding/binary"
	"testing"
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
	if err != nil {
		t.Fatalf("WriteTo() error = %v", err)
	}
	if n != headerSize {
		t.Fatalf("WriteTo() bytes = %d, want %d", n, headerSize)
	}

	got := buf.Bytes()
	if len(got) != headerSize {
		t.Fatalf("len(buf) = %d, want %d", len(got), headerSize)
	}
	if got[0] != header.Version {
		t.Fatalf("version byte = %d, want %d", got[0], header.Version)
	}
	if PType(got[1]) != header.PType {
		t.Fatalf("ptype byte = %v, want %v", PType(got[1]), header.PType)
	}
	if got := binary.LittleEndian.Uint16(got[2:4]); got != header.Flags {
		t.Fatalf("flags = %#x, want %#x", got, header.Flags)
	}
	if got := binary.LittleEndian.Uint64(got[4:12]); got != header.Length {
		t.Fatalf("length = %#x, want %#x", got, header.Length)
	}
	if got := binary.LittleEndian.Uint64(got[12:20]); got != header.BodySize {
		t.Fatalf("body size = %#x, want %#x", got, header.BodySize)
	}
}
