package btrblocks

import (
	"bytes"
	"io"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

var benchSizes = []int{1_000, 10_000, 100_000, 1_000_000}

func mustWriteEncodedArray(t testing.TB, codec io.WriterTo) []byte {
	t.Helper()
	var buf bytes.Buffer
	_, err := codec.WriteTo(&buf)
	require.NoError(t, err)
	return buf.Bytes()
}

func equalFloats[T Float](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !cmpFloatBits(a[i], b[i]) {
			return false
		}
	}
	return true
}

func assertValuesEqual[T Integer | Float | String](t *testing.T, want, got []T) {
	t.Helper()
	var zero T
	switch any(zero).(type) {
	case float32:
		require.True(t, equalFloats(any(want).([]float32), any(got).([]float32)), "float32 values mismatch")
	case float64:
		require.True(t, equalFloats(any(want).([]float64), any(got).([]float64)), "float64 values mismatch")
	default:
		require.Equal(t, want, got)
	}
}

func assertValueEqual[T Integer | Float | String](t *testing.T, want, got T, idx uint64) {
	t.Helper()
	var zero T
	switch any(zero).(type) {
	case float32:
		require.Equal(t, math.Float32bits(any(want).(float32)), math.Float32bits(any(got).(float32)), "ValueAt(%d)", idx)
	case float64:
		require.Equal(t, math.Float64bits(any(want).(float64)), math.Float64bits(any(got).(float64)), "ValueAt(%d)", idx)
	default:
		require.Equal(t, want, got, "ValueAt(%d)", idx)
	}
}

func spotCheckIndices(length uint64) []uint64 {
	if length == 0 {
		return nil
	}
	indices := []uint64{0}
	if length > 1 {
		indices = append(indices, length-1)
	}
	if length > 2 {
		indices = append(indices, length/2)
	}
	if length > 10 {
		indices = append(indices, length/4, 3*length/4)
	}
	return indices
}

// assertRoundTrip verifies the full codec lifecycle: Decompress before
// serialization, BinarySize consistency, WriteTo -> Load -> Decompress,
// DecompressInto, and ValueAt spot checks on the deserialized codec.
func assertRoundTrip[T Integer | Float | String](t *testing.T, encoded EncodedArray[T], want []T) {
	t.Helper()

	// Decompress before serialization.
	decoded, err := Decompress(encoded)
	require.NoError(t, err)
	assertValuesEqual(t, want, decoded)

	// BinarySize matches actual bytes written.
	data := mustWriteEncodedArray(t, encoded)
	require.Equal(t, encoded.BinarySize(), uint64(len(data)), "BinarySize vs WriteTo mismatch")

	// Load from serialized bytes.
	loaded, err := Load[T](data)
	require.NoError(t, err)
	require.Equal(t, encoded.Length(), loaded.Length())
	require.Equal(t, encoded.Encoding(), loaded.Encoding())

	// Decompress after Load.
	decoded, err = Decompress(loaded)
	require.NoError(t, err)
	assertValuesEqual(t, want, decoded)

	// DecompressInto after Load.
	dst := make([]T, loaded.Length())
	err = loaded.DecompressInto(dst)
	require.NoError(t, err)
	assertValuesEqual(t, want, dst)

	// ValueAt spot checks on loaded codec.
	for _, idx := range spotCheckIndices(loaded.Length()) {
		assertValueEqual(t, want[idx], loaded.ValueAt(idx), idx)
	}
}

// assertSliceRoundTrip verifies that Slice produces correct values and
// survives a WriteTo -> Load round-trip.
func assertSliceRoundTrip[T Integer | Float | String](t *testing.T, encoded EncodedArray[T], start, end uint64, want []T) {
	t.Helper()

	sliced, err := encoded.Slice(start, end)
	require.NoError(t, err)
	require.Equal(t, end-start, sliced.Length())

	decoded, err := Decompress(sliced)
	require.NoError(t, err)
	assertValuesEqual(t, want[start:end], decoded)

	// Round-trip the slice through serialization.
	data := mustWriteEncodedArray(t, sliced)
	loaded, err := Load[T](data)
	require.NoError(t, err)

	decoded, err = Decompress(loaded)
	require.NoError(t, err)
	assertValuesEqual(t, want[start:end], decoded)
}

func benchDecompress[T Integer | Float | String](b *testing.B, encoded EncodedArray[T]) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Decompress(encoded); err != nil {
			b.Fatal(err)
		}
	}
}

func benchDecompressInto[T Integer | Float | String](b *testing.B, encoded EncodedArray[T]) {
	dst := make([]T, encoded.Length())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := encoded.DecompressInto(dst); err != nil {
			b.Fatal(err)
		}
	}
}

func serializeForRead[T Integer | Float | String](values []T, opts Options) []byte {
	codec, err := Compress(buildArray(values), opts)
	if err != nil {
		panic(err)
	}
	var buf bytes.Buffer
	if _, err := codec.WriteTo(&buf); err != nil {
		panic(err)
	}
	return buf.Bytes()
}
