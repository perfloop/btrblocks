package array

import (
	"bytes"
	"testing"
)

func TestVirtualWriteToRoundTrip(t *testing.T) {
	nulls := []bool{false, true, false, true, false}
	physical := NewVirtual(uint64(len(nulls)), func(i uint64) uint8 {
		if nulls[i] {
			return 1
		}
		return 0
	})
	for i, want := range []uint8{0, 1, 0, 1, 0} {
		if got := physical.ValueAt(uint64(i)); got != want {
			t.Fatalf("ValueAt(%d) = %d, want %d", i, got, want)
		}
	}

	var buf bytes.Buffer
	n, err := physical.WriteTo(&buf)
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if got, want := uint64(n), physical.BinarySize(); got != want {
		t.Fatalf("WriteTo bytes = %d, want %d", got, want)
	}
	loaded, err := ReadPrimitiveFromBuf[uint8](&BufReader{Buf: buf.Bytes()})
	if err != nil {
		t.Fatalf("ReadPrimitiveFromBuf: %v", err)
	}
	for i, want := range []uint8{0, 1, 0, 1, 0} {
		if got := loaded.ValueAt(uint64(i)); got != want {
			t.Fatalf("loaded ValueAt(%d) = %d, want %d", i, got, want)
		}
	}
}

func TestVirtualWriteToSpansChunks(t *testing.T) {
	const length = 1024*3 + 1
	physical := NewVirtual(length, func(i uint64) uint8 { return uint8(i) })

	var buf bytes.Buffer
	n, err := physical.WriteTo(&buf)
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if got, want := uint64(n), physical.BinarySize(); got != want {
		t.Fatalf("WriteTo bytes = %d, want %d", got, want)
	}
	loaded, err := ReadPrimitiveFromBuf[uint8](&BufReader{Buf: buf.Bytes()})
	if err != nil {
		t.Fatalf("ReadPrimitiveFromBuf: %v", err)
	}
	for i := range uint64(length) {
		if got := loaded.ValueAt(i); got != uint8(i) {
			t.Fatalf("loaded ValueAt(%d) = %d, want %d", i, got, uint8(i))
		}
	}
}
