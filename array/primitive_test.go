package array

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewPrimitivesCopiesInput(t *testing.T) {
	values := []uint32{1, 2, 3}

	arr := NewPrimitives(values)
	values[0] = 99

	require.Equal(t, uint32(1), arr.ValueAt(0))
}

func TestNewPrimitivesUnsafeSharesInput(t *testing.T) {
	values := []uint32{1, 2, 3}

	arr := NewPrimitivesUnsafe(values)
	values[0] = 99

	require.Equal(t, uint32(99), arr.ValueAt(0))
}

func TestPrimitiveMetadata(t *testing.T) {
	t.Run("int32", func(t *testing.T) {
		assertPrimitiveMetadata(t, []int32{-3, 5, 8}, PTypeInt32, 32)
	})
	t.Run("float32", func(t *testing.T) {
		assertPrimitiveMetadata(t, []float32{1.5, -2.25}, PTypeFloat32, 28)
	})
	t.Run("float64", func(t *testing.T) {
		assertPrimitiveMetadata(t, []float64{1.5, -2.25}, PTypeFloat64, 36)
	})
}

func TestPrimitiveWriteToIncludesHeaderAndBody(t *testing.T) {
	values := []uint16{7, 42, 1024}
	arr := NewPrimitives(values)
	wantBody := encodePrimitiveBody(t, values)

	var buf bytes.Buffer
	n, err := arr.WriteTo(&buf)
	require.NoError(t, err)
	require.EqualValues(t, headerSize+len(wantBody), n)

	got := buf.Bytes()
	assertHeaderBytes(t, got[:headerSize], Header{
		Version: 1,
		PType:   PTypeUint16,
		Length:  uint64(len(values)),
		NBytes:  uint64(len(wantBody)),
	})
	require.Equal(t, wantBody, got[headerSize:])
}

func TestPrimitivesSlice(t *testing.T) {
	values := []uint32{1, 2, 3, 4}
	arr := NewPrimitivesUnsafe(values)

	sliced, err := arr.Slice(1, 3)
	require.NoError(t, err)
	require.Equal(t, uint64(2), sliced.Length())
	require.Equal(t, uint32(2), sliced.ValueAt(0))
	require.Equal(t, uint32(3), sliced.ValueAt(1))

	values[1] = 9
	require.Equal(t, uint32(9), sliced.ValueAt(0))
}

func TestPrimitivesLargeCorpus(t *testing.T) {
	values := make([]uint32, largeCorpusSize)
	for i := range values {
		values[i] = uint32(i*3 + 1)
	}

	arr := NewPrimitives(values)
	require.EqualValues(t, largeCorpusSize, arr.Length())
	require.EqualValues(t, headerSize+largeCorpusSize*4, arr.BinarySize())

	for _, idx := range []uint64{0, 1, largeCorpusSize / 2, largeCorpusSize - 1} {
		require.Equal(t, values[idx], arr.ValueAt(idx), "ValueAt(%d)", idx)
	}

	n, err := arr.WriteTo(io.Discard)
	require.NoError(t, err)
	require.EqualValues(t, arr.BinarySize(), n)
}

func TestReadPrimitives(t *testing.T) {
	t.Run("int32 round-trip", func(t *testing.T) {
		values := []int32{-1, 0, 42, 1 << 20}
		arr := NewPrimitives(values)
		var buf bytes.Buffer
		_, err := arr.WriteTo(&buf)
		require.NoError(t, err)
		got, err := ReadPrimitives[int32](&buf)
		require.NoError(t, err)
		require.Equal(t, arr.Length(), got.Length())
		for i := uint64(0); i < got.Length(); i++ {
			require.Equal(t, values[i], got.ValueAt(i), "ValueAt(%d)", i)
		}
	})
	t.Run("empty", func(t *testing.T) {
		arr := NewPrimitives([]float64{})
		var buf bytes.Buffer
		_, err := arr.WriteTo(&buf)
		require.NoError(t, err)
		got, err := ReadPrimitives[float64](&buf)
		require.NoError(t, err)
		require.Equal(t, uint64(0), got.Length())
	})
}

func TestReadPrimitivesRejectsUnsupportedHeader(t *testing.T) {
	tests := []struct {
		name   string
		header Header
		want   string
	}{
		{
			name:   "version",
			header: Header{Version: 2, PType: PTypeInt32, Length: 0, NBytes: 0},
			want:   "version",
		},
		{
			name:   "flags",
			header: Header{Version: 1, PType: PTypeInt32, Flags: 1, Length: 0, NBytes: 0},
			want:   "flags",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			_, err := tt.header.WriteTo(&buf)
			require.NoError(t, err)

			_, err = ReadPrimitives[int32](bytes.NewReader(buf.Bytes()))
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestReadPrimitivesRejectsZeroLengthWithBody(t *testing.T) {
	var buf bytes.Buffer
	_, err := Header{
		Version: 1,
		PType:   PTypeUint32,
		Length:  0,
		NBytes:  4,
	}.WriteTo(&buf)
	require.NoError(t, err)

	_, err = ReadPrimitives[uint32](bytes.NewReader(buf.Bytes()))
	require.ErrorContains(t, err, "body size")
}

func TestPrimitivesWriteToShortWrite(t *testing.T) {
	arr := NewPrimitives([]uint16{7, 42, 1024})
	writer := &shortWriter{remaining: int(arr.BinarySize()) - 1}

	n, err := arr.WriteTo(writer)
	require.ErrorIs(t, err, io.ErrShortWrite)
	require.EqualValues(t, arr.BinarySize()-1, n)
}

func assertPrimitiveMetadata[T PrimitiveType](t *testing.T, values []T, wantPType PType, wantBinarySize uint64) {
	t.Helper()

	arr := NewPrimitives(values)
	require.Equal(t, wantPType, arr.PType())
	require.Equal(t, uint64(len(values)), arr.Length())
	require.Equal(t, wantBinarySize, arr.BinarySize())
	for i, want := range values {
		require.Equal(t, want, arr.ValueAt(uint64(i)), "ValueAt(%d)", i)
	}
}

func encodePrimitiveBody[T PrimitiveType](t *testing.T, values []T) []byte {
	t.Helper()

	var buf bytes.Buffer
	require.NoError(t, binary.Write(&buf, binary.LittleEndian, values))
	return buf.Bytes()
}

func assertHeaderBytes(t *testing.T, got []byte, want Header) {
	t.Helper()

	require.Len(t, got, headerSize)
	require.Equal(t, want.Version, got[0])
	require.Equal(t, want.PType, PType(got[1]))
	require.Equal(t, want.Flags, binary.LittleEndian.Uint16(got[2:4]))
	require.Equal(t, want.Length, binary.LittleEndian.Uint64(got[4:12]))
	require.Equal(t, want.NBytes, binary.LittleEndian.Uint64(got[12:20]))
}

func BenchmarkWritePrimitives(b *testing.B) {
	values := make([]int32, 10000)
	for i := range values {
		values[i] = int32(i)
	}
	arr := NewPrimitives(values)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = arr.WriteTo(io.Discard)
	}
}

func BenchmarkReadPrimitives(b *testing.B) {
	values := make([]int32, 10000)
	for i := range values {
		values[i] = int32(i)
	}
	arr := NewPrimitives(values)
	var buf bytes.Buffer
	_, _ = arr.WriteTo(&buf)
	data := buf.Bytes()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ReadPrimitives[int32](bytes.NewReader(data))
	}
}

func BenchmarkValueAtPrimitives(b *testing.B) {
	values := make([]uint64, 10000)
	for i := range values {
		values[i] = uint64(i)
	}
	arr := NewPrimitives(values)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = arr.ValueAt(uint64(i % 10000))
	}
}

func FuzzReadPrimitives(f *testing.F) {
	// Seed with valid encoded primitive array so corpus has at least one valid input.
	arr := NewPrimitives([]int32{1, 2, 3})
	var buf bytes.Buffer
	_, _ = arr.WriteTo(&buf)
	f.Add(buf.Bytes())
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ReadPrimitives[int32](bytes.NewReader(data))
		// Must not panic; error is acceptable for invalid/corrupt input.
	})
}
