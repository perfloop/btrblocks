package array

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

func TestNewPrimitivesCopiesInput(t *testing.T) {
	values := []uint32{1, 2, 3}

	arr := NewPrimitives(values)
	values[0] = 99

	if got := arr.ValueAt(0); got != 1 {
		t.Fatalf("ValueAt(0) = %d, want 1", got)
	}
}

func TestNewPrimitivesUnsafeSharesInput(t *testing.T) {
	values := []uint32{1, 2, 3}

	arr := NewPrimitivesUnsafe(values)
	values[0] = 99

	if got := arr.ValueAt(0); got != 99 {
		t.Fatalf("ValueAt(0) = %d, want 99", got)
	}
}

func TestPrimitiveMetadata(t *testing.T) {
	t.Run("int32", func(t *testing.T) {
		assertPrimitiveMetadata(t, []int32{-3, 5, 8}, PTypeInt32, 12)
	})
	t.Run("float32", func(t *testing.T) {
		assertPrimitiveMetadata(t, []float32{1.5, -2.25}, PTypeFloat32, 8)
	})
	t.Run("float64", func(t *testing.T) {
		assertPrimitiveMetadata(t, []float64{1.5, -2.25}, PTypeFloat64, 16)
	})
}

func TestPrimitiveWriteToIncludesHeaderAndBody(t *testing.T) {
	values := []uint16{7, 42, 1024}
	arr := NewPrimitives(values)
	wantBody := encodePrimitiveBody(t, values)

	var buf bytes.Buffer
	n, err := arr.WriteTo(&buf)
	if err != nil {
		t.Fatalf("WriteTo() error = %v", err)
	}
	if want := int64(headerSize + len(wantBody)); n != want {
		t.Fatalf("WriteTo() bytes = %d, want %d", n, want)
	}

	got := buf.Bytes()
	assertHeaderBytes(t, got[:headerSize], Header{
		Version:  1,
		PType:    PTypeUint16,
		Length:   uint64(len(values)),
		BodySize: uint64(len(wantBody)),
	})
	if !bytes.Equal(got[headerSize:], wantBody) {
		t.Fatalf("body bytes = %v, want %v", got[headerSize:], wantBody)
	}
}

func TestPrimitivesLargeCorpus(t *testing.T) {
	values := make([]uint32, largeCorpusSize)
	for i := range values {
		values[i] = uint32(i*3 + 1)
	}

	arr := NewPrimitives(values)
	if got := arr.Length(); got != largeCorpusSize {
		t.Fatalf("Length() = %d, want %d", got, largeCorpusSize)
	}
	if got := arr.BinarySize(); got != uint64(largeCorpusSize*4) {
		t.Fatalf("BinarySize() = %d, want %d", got, largeCorpusSize*4)
	}

	for _, idx := range []uint64{0, 1, largeCorpusSize / 2, largeCorpusSize - 1} {
		if got := arr.ValueAt(idx); got != values[idx] {
			t.Fatalf("ValueAt(%d) = %d, want %d", idx, got, values[idx])
		}
	}

	n, err := arr.WriteTo(io.Discard)
	if err != nil {
		t.Fatalf("WriteTo() error = %v", err)
	}
	if want := int64(headerSize + arr.BinarySize()); n != want {
		t.Fatalf("WriteTo() bytes = %d, want %d", n, want)
	}
}

func assertPrimitiveMetadata[T PrimitiveType](t *testing.T, values []T, wantPType PType, wantBinarySize uint64) {
	t.Helper()

	arr := NewPrimitives(values)
	if got := arr.PType(); got != wantPType {
		t.Fatalf("PType() = %v, want %v", got, wantPType)
	}
	if got := arr.Length(); got != uint64(len(values)) {
		t.Fatalf("Length() = %d, want %d", got, len(values))
	}
	if got := arr.BinarySize(); got != wantBinarySize {
		t.Fatalf("BinarySize() = %d, want %d", got, wantBinarySize)
	}
	for i, want := range values {
		if got := arr.ValueAt(uint64(i)); got != want {
			t.Fatalf("ValueAt(%d) = %v, want %v", i, got, want)
		}
	}
}

func encodePrimitiveBody[T PrimitiveType](t *testing.T, values []T) []byte {
	t.Helper()

	var buf bytes.Buffer
	if err := binary.Write(&buf, binary.LittleEndian, values); err != nil {
		t.Fatalf("binary.Write() error = %v", err)
	}
	return buf.Bytes()
}

func assertHeaderBytes(t *testing.T, got []byte, want Header) {
	t.Helper()

	if len(got) != headerSize {
		t.Fatalf("len(header) = %d, want %d", len(got), headerSize)
	}
	if got[0] != want.Version {
		t.Fatalf("version byte = %d, want %d", got[0], want.Version)
	}
	if PType(got[1]) != want.PType {
		t.Fatalf("ptype byte = %v, want %v", PType(got[1]), want.PType)
	}
	if got := binary.LittleEndian.Uint16(got[2:4]); got != want.Flags {
		t.Fatalf("flags = %#x, want %#x", got, want.Flags)
	}
	if got := binary.LittleEndian.Uint64(got[4:12]); got != want.Length {
		t.Fatalf("length = %d, want %d", got, want.Length)
	}
	if got := binary.LittleEndian.Uint64(got[12:20]); got != want.BodySize {
		t.Fatalf("body size = %d, want %d", got, want.BodySize)
	}
}
