package codec

import (
	"bytes"
	"encoding"
	"errors"
	"io"
	"slices"
	"testing"

	"github.com/axiomhq/btrblocks/array"
)

var _ encoding.BinaryMarshaler = (EncodedArray[uint64])(nil)

var benchSizes = []int{1_000, 10_000, 100_000, 1_000_000}

type marshalTestValue struct {
	size  uint64
	write func(io.Writer) (int64, error)
}

func (v marshalTestValue) BinarySize() uint64 { return v.size }
func (v marshalTestValue) WriteTo(w io.Writer) (int64, error) {
	return v.write(w)
}

func TestMarshalBinaryMatchesWriteToAndOwnsResult(t *testing.T) {
	encoded := newRawArray(array.NewPrimitivesUnsafe([]uint64{1, 2, 3}))
	want := mustWriteEncodedArray(t, encoded)

	got, err := encoded.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("MarshalBinary = %x, WriteTo = %x", got, want)
	}
	if uint64(len(got)) != encoded.BinarySize() {
		t.Fatalf("MarshalBinary length = %d, want %d", len(got), encoded.BinarySize())
	}

	loaded, err := LoadUnsigned[uint64](got)
	if err != nil {
		t.Fatalf("LoadUnsigned: %v", err)
	}
	values, err := Decompress(loaded)
	if err != nil {
		t.Fatalf("Decompress: %v", err)
	}
	if !slices.Equal(values, []uint64{1, 2, 3}) {
		t.Fatalf("round-trip values = %v, want [1 2 3]", values)
	}

	got[0] ^= 0xff
	again, err := encoded.MarshalBinary()
	if err != nil {
		t.Fatalf("second MarshalBinary: %v", err)
	}
	if !bytes.Equal(again, want) {
		t.Fatalf("mutating returned bytes changed encoded array: got %x, want %x", again, want)
	}
}

func TestMarshalBinaryRejectsUnsafeSizesAndWrites(t *testing.T) {
	t.Run("size overflows int", func(t *testing.T) {
		value := marshalTestValue{
			size: ^uint64(0),
			write: func(io.Writer) (int64, error) {
				t.Fatal("MarshalBinary called WriteTo after size overflow")
				return 0, nil
			},
		}
		if data, err := marshalBinary(value); err == nil || data != nil {
			t.Fatalf("marshalBinary = (%v, %v), want (nil, error)", data, err)
		}
	})

	t.Run("writer error", func(t *testing.T) {
		wantErr := errors.New("write failed")
		value := marshalTestValue{size: 1, write: func(io.Writer) (int64, error) {
			return 0, wantErr
		}}
		data, err := marshalBinary(value)
		if !errors.Is(err, wantErr) || data != nil {
			t.Fatalf("marshalBinary = (%v, %v), want (nil, wrapped error)", data, err)
		}
	})

	t.Run("write exceeds declared size", func(t *testing.T) {
		value := marshalTestValue{size: 1, write: func(w io.Writer) (int64, error) {
			n, err := w.Write([]byte{1, 2})
			return int64(n), err
		}}
		data, err := marshalBinary(value)
		if !errors.Is(err, io.ErrShortWrite) || data != nil {
			t.Fatalf("marshalBinary = (%v, %v), want (nil, %v)", data, err, io.ErrShortWrite)
		}
	})

	t.Run("reported count mismatch", func(t *testing.T) {
		value := marshalTestValue{size: 1, write: func(w io.Writer) (int64, error) {
			if _, err := w.Write([]byte{1}); err != nil {
				return 0, err
			}
			return 0, nil
		}}
		if data, err := marshalBinary(value); err == nil || data != nil {
			t.Fatalf("marshalBinary = (%v, %v), want (nil, error)", data, err)
		}
	})

	t.Run("buffered count mismatch", func(t *testing.T) {
		value := marshalTestValue{size: 2, write: func(w io.Writer) (int64, error) {
			if _, err := w.Write([]byte{1}); err != nil {
				return 0, err
			}
			return 2, nil
		}}
		if data, err := marshalBinary(value); err == nil || data != nil {
			t.Fatalf("marshalBinary = (%v, %v), want (nil, error)", data, err)
		}
	})
}

func TestDecompressRejectsLargeCompactOutput(t *testing.T) {
	data := mustWriteEncodedArray(t, &constArray[uint64]{denseRows: 1 << 26, body: array.NewPrimitivesUnsafe([]uint64{1})})
	encoded, err := LoadUnsigned[uint64](data)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := Decompress(encoded); !errors.Is(err, ErrMaterializationLimit) {
		t.Fatalf("Decompress error = %v, want %v", err, ErrMaterializationLimit)
	}
}

func benchDecompress[T Integer | Float | String](b *testing.B, encoded EncodedArray[T]) {
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := Decompress(encoded); err != nil {
			b.Fatal(err)
		}
	}
}

func benchDecompressInto[T Integer | Float | String](b *testing.B, encoded EncodedArray[T]) {
	dst := make([]T, encoded.Length())
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := encoded.DecompressInto(dst); err != nil {
			b.Fatal(err)
		}
	}
}
