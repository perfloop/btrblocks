package array

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

const largeCorpusSize = 1 << 20

func TestArrayContractWithPrimitives(t *testing.T) {
	assertArrayContract(t, NewPrimitives([]uint16{3, 5, 8}), []uint16{3, 5, 8}, PTypeUint16, 26, 26)
}

func TestArrayContractWithStrings(t *testing.T) {
	assertArrayContract(t, NewStrings([]string{"go", "", "lang"}), []string{"go", "", "lang"}, PTypeString, 34, 34)
}

func TestReadArrayTypeMismatchReturnsError(t *testing.T) {
	arr := NewStrings([]string{"go", "lang"})

	var buf bytes.Buffer
	_, err := arr.WriteTo(&buf)
	require.NoError(t, err)

	_, err = ReadArray[uint64](bytes.NewReader(buf.Bytes()))
	require.ErrorContains(t, err, "PType")
}

func assertArrayContract[T Integer | Float | String](t *testing.T, arr Array[T], want []T, wantPType PType, wantBinarySize uint64, wantWriteSize int64) {
	t.Helper()

	require.Equal(t, uint64(len(want)), arr.Length())
	require.Equal(t, wantPType, arr.PType())
	require.Equal(t, wantBinarySize, arr.BinarySize())
	for i, wantValue := range want {
		require.Equal(t, wantValue, arr.ValueAt(uint64(i)), "ValueAt(%d)", i)
	}
	n, err := arr.WriteTo(io.Discard)
	require.NoError(t, err)
	require.Equal(t, wantWriteSize, n)
}

func BenchmarkArrayWritePrimitives(b *testing.B) {
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

func BenchmarkArrayWriteStrings(b *testing.B) {
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

func BenchmarkArrayValueAtPrimitives(b *testing.B) {
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

func BenchmarkArrayValueAtStrings(b *testing.B) {
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

func FuzzArrayRoundTripPrimitives(f *testing.F) {
	seeds := [][]int32{
		{},
		{1, 2, 3},
		{-1, 0, 1},
	}
	for _, s := range seeds {
		f.Add(len(s))
	}
	f.Fuzz(func(t *testing.T, n int) {
		if n < 0 || n > 100000 {
			return
		}
		values := make([]int32, n)
		for i := range values {
			values[i] = int32(i)
		}
		arr := NewPrimitives(values)
		var buf bytes.Buffer
		_, err := arr.WriteTo(&buf)
		if err != nil {
			t.Fatalf("write: %v", err)
		}
		got, err := ReadPrimitives[int32](&buf)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if got.Length() != uint64(n) {
			t.Errorf("length: got %d want %d", got.Length(), n)
		}
		for i := uint64(0); i < got.Length(); i++ {
			if got.ValueAt(i) != values[i] {
				t.Errorf("ValueAt(%d): got %d want %d", i, got.ValueAt(i), values[i])
			}
		}
	})
}
