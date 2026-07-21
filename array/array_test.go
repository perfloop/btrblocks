package array

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

const largeCorpusSize = 1 << 20

func mustStrings(t testing.TB, values []string) Array[string] {
	t.Helper()
	arr, err := NewStrings(values)
	if err != nil {
		t.Fatalf("NewStrings: %v", err)
	}
	return arr
}

func readPrimitiveFromBytes[T PrimitiveType](data []byte, opts ...ReadOptions) (Array[T], error) {
	return ReadPrimitiveFromBuf[T](&BufReader{Buf: data}, opts...)
}

func readStringsFromBytes(data []byte, opts ...ReadOptions) (Array[string], error) {
	return ReadStringsFromBuf(&BufReader{Buf: data}, opts...)
}

type arrayReader[T Integer | Float | String] func(*BufReader, ...ReadOptions) (Array[T], error)

func primitiveArrayReader[T PrimitiveType](br *BufReader, opts ...ReadOptions) (Array[T], error) {
	return ReadPrimitiveFromBuf[T](br, opts...)
}

func stringArrayReader(br *BufReader, opts ...ReadOptions) (Array[string], error) {
	return ReadStringsFromBuf(br, opts...)
}

func TestArrayContractWithPrimitives(t *testing.T) {
	assertArrayContract(t, NewPrimitives([]uint16{3, 5, 8}), []uint16{3, 5, 8}, PTypeUint16, 26, 26)
}

func TestArrayContractWithStrings(t *testing.T) {
	assertArrayContract(t, mustStrings(t, []string{"go", "", "lang"}), []string{"go", "", "lang"}, PTypeString, 34, 34)
}

func TestReadArrayFromBufTypeMismatchReturnsError(t *testing.T) {
	arr := mustStrings(t, []string{"go", "lang"})

	var buf bytes.Buffer
	_, err := arr.WriteTo(&buf)
	require.NoError(t, err)

	_, err = readPrimitiveFromBytes[uint64](buf.Bytes())
	require.ErrorContains(t, err, "PType")
}

func TestReadArrayFromBufRejectsUnsupportedHeader(t *testing.T) {
	tests := []struct {
		name   string
		header Header
		want   string
	}{
		{
			name:   "version",
			header: Header{Version: 3, PType: PTypeUint64, Length: 0, NumBytes: 0},
			want:   "version",
		},
		{
			name:   "flags",
			header: Header{Version: FormatVersion, PType: PTypeUint64, Flags: 2, Length: 0, NumBytes: 0},
			want:   "flags",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			_, err := tt.header.WriteTo(&buf)
			require.NoError(t, err)

			_, err = readPrimitiveFromBytes[uint64](buf.Bytes())
			require.ErrorContains(t, err, tt.want)
		})
	}
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
	for b.Loop() {
		_, _ = arr.WriteTo(io.Discard)
	}
}

func BenchmarkArrayWriteStrings(b *testing.B) {
	values := make([]string, 1000)
	for i := range values {
		values[i] = "hello"
	}
	arr := mustStrings(b, values)
	b.ResetTimer()
	for b.Loop() {
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
	var offset uint64
	for b.Loop() {
		_ = arr.ValueAt(offset % 10000)
		offset++
	}
}

func BenchmarkArrayValueAtStrings(b *testing.B) {
	values := make([]string, 1000)
	for i := range values {
		values[i] = "hello"
	}
	arr := mustStrings(b, values)
	b.ResetTimer()
	var offset uint64
	for b.Loop() {
		_ = arr.ValueAt(offset % 1000)
		offset++
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
		got, err := readPrimitiveFromBytes[int32](buf.Bytes())
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if got.Length() != uint64(n) {
			t.Errorf("length: got %d want %d", got.Length(), n)
		}
		for i := range got.Length() {
			if got.ValueAt(i) != values[i] {
				t.Errorf("ValueAt(%d): got %d want %d", i, got.ValueAt(i), values[i])
			}
		}
	})
}

func benchmarkArrayIO[T Integer | Float | String](b *testing.B, typeName, distribution string, arr Array[T], read arrayReader[T]) {
	b.Helper()
	var encoded bytes.Buffer
	if _, err := arr.WriteTo(&encoded); err != nil {
		b.Fatal(err)
	}
	data := append([]byte(nil), encoded.Bytes()...)

	name := fmt.Sprintf("type=%s/distribution=%s/rows=%d", typeName, distribution, arr.Length())
	b.Run(name+"/operation=serialize", func(b *testing.B) {
		var buf bytes.Buffer
		buf.Grow(len(data))
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			buf.Reset()
			if _, err := arr.WriteTo(&buf); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run(name+"/operation=read", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			if _, err := read(&BufReader{Buf: data}); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run(name+"/operation=value_at", func(b *testing.B) {
		var offset uint64
		b.ResetTimer()
		for b.Loop() {
			_ = arr.ValueAt(offset % arr.Length())
			offset++
		}
	})
}

func BenchmarkScalarArrayIO(b *testing.B) {
	const rows = 4096

	int8Values := make([]int8, rows)
	int16Values := make([]int16, rows)
	int32Values := make([]int32, rows)
	int64Values := make([]int64, rows)
	uint8Values := make([]uint8, rows)
	uint16Values := make([]uint16, rows)
	uint32Values := make([]uint32, rows)
	uint64Values := make([]uint64, rows)
	float32Values := make([]float32, rows)
	float64Values := make([]float64, rows)
	for i := range rows {
		int8Values[i] = int8(i%31) - 15
		int16Values[i] = int16(i%257) - 128
		int32Values[i] = int32(i%4097) - 2048
		int64Values[i] = int64(i%65537) - 32768
		uint8Values[i] = uint8(i % 32)
		uint16Values[i] = uint16(i % 1024)
		uint32Values[i] = uint32(i%4096) * 17
		uint64Values[i] = 1<<48 + uint64(i%256)
		float32Values[i] = float32(i%1000) * 0.125
		float64Values[i] = float64(i%1000)*0.001 - 100
	}

	benchmarkArrayIO(b, "int8", "signed_small", NewPrimitives(int8Values), primitiveArrayReader[int8])
	benchmarkArrayIO(b, "int16", "signed_small", NewPrimitives(int16Values), primitiveArrayReader[int16])
	benchmarkArrayIO(b, "int32", "signed_small", NewPrimitives(int32Values), primitiveArrayReader[int32])
	benchmarkArrayIO(b, "int64", "signed_small", NewPrimitives(int64Values), primitiveArrayReader[int64])
	benchmarkArrayIO(b, "uint8", "low_cardinality", NewPrimitives(uint8Values), primitiveArrayReader[uint8])
	benchmarkArrayIO(b, "uint16", "low_cardinality", NewPrimitives(uint16Values), primitiveArrayReader[uint16])
	benchmarkArrayIO(b, "uint32", "low_cardinality", NewPrimitives(uint32Values), primitiveArrayReader[uint32])
	benchmarkArrayIO(b, "uint64", "narrow_high_base", NewPrimitives(uint64Values), primitiveArrayReader[uint64])
	benchmarkArrayIO(b, "float32", "decimal", NewPrimitives(float32Values), primitiveArrayReader[float32])
	benchmarkArrayIO(b, "float64", "decimal", NewPrimitives(float64Values), primitiveArrayReader[float64])

	repeated := make([]string, rows)
	prefix := make([]string, rows)
	unique := make([]string, rows)
	for i := range rows {
		repeated[i] = []string{"", "ok", "warn", "error"}[i%4]
		prefix[i] = fmt.Sprintf("/api/v1/resource/%d", i%64)
		unique[i] = fmt.Sprintf("event-%08d", i)
	}
	benchmarkArrayIO(b, "string", "repeated", mustStrings(b, repeated), stringArrayReader)
	benchmarkArrayIO(b, "string", "common_prefix", mustStrings(b, prefix), stringArrayReader)
	benchmarkArrayIO(b, "string", "unique", mustStrings(b, unique), stringArrayReader)
}

func FuzzArrayEncodedBytesAllTypes(f *testing.F) {
	validPrimitive := NewPrimitives([]int32{-1, 0, 1})
	var primitiveBytes bytes.Buffer
	_, _ = validPrimitive.WriteTo(&primitiveBytes)
	validStrings := mustStrings(f, []string{"", "alpha", "beta"})
	var stringBytes bytes.Buffer
	_, _ = validStrings.WriteTo(&stringBytes)
	f.Add(primitiveBytes.Bytes())
	f.Add(stringBytes.Bytes())
	f.Add([]byte(nil))
	f.Add(make([]byte, HeaderSize))

	f.Fuzz(func(t *testing.T, data []byte) {
		opts := ReadOptions{MaxLength: 1 << 10, MaxBytes: 1 << 20}
		_, _ = ReadPrimitiveFromBuf[int8](&BufReader{Buf: data}, opts)
		_, _ = ReadPrimitiveFromBuf[int16](&BufReader{Buf: data}, opts)
		_, _ = ReadPrimitiveFromBuf[int32](&BufReader{Buf: data}, opts)
		_, _ = ReadPrimitiveFromBuf[int64](&BufReader{Buf: data}, opts)
		_, _ = ReadPrimitiveFromBuf[uint8](&BufReader{Buf: data}, opts)
		_, _ = ReadPrimitiveFromBuf[uint16](&BufReader{Buf: data}, opts)
		_, _ = ReadPrimitiveFromBuf[uint32](&BufReader{Buf: data}, opts)
		_, _ = ReadPrimitiveFromBuf[uint64](&BufReader{Buf: data}, opts)
		_, _ = ReadPrimitiveFromBuf[float32](&BufReader{Buf: data}, opts)
		_, _ = ReadPrimitiveFromBuf[float64](&BufReader{Buf: data}, opts)
		_, _ = ReadStringsFromBuf(&BufReader{Buf: data}, opts)
	})
}

func FuzzArrayMalformedHeaders(f *testing.F) {
	arr := NewPrimitives([]uint32{1, 2, 3})
	var encoded bytes.Buffer
	_, _ = arr.WriteTo(&encoded)
	f.Add(encoded.Bytes())
	f.Add(make([]byte, HeaderSize+8))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < HeaderSize {
			return
		}
		for _, offset := range []int{0, 1, 2, 4, 12} {
			mutated := append([]byte(nil), data...)
			mutated[offset] ^= 0xff
			_, _ = ReadPrimitiveFromBuf[uint32](&BufReader{Buf: mutated})
			_, _ = ReadStringsFromBuf(&BufReader{Buf: mutated})
		}
	})
}

func FuzzArrayReadLimits(f *testing.F) {
	f.Add([]byte(nil), uint64(0), uint64(0))
	f.Add(make([]byte, HeaderSize), uint64(3), uint64(16))
	f.Fuzz(func(t *testing.T, data []byte, maxLength, maxBytes uint64) {
		opts := ReadOptions{
			MaxLength: maxLength % 1025,
			MaxBytes:  maxBytes % (1 << 20),
		}
		_, _ = ReadPrimitiveFromBuf[int32](&BufReader{Buf: data}, opts)
		_, _ = ReadPrimitiveFromBuf[float64](&BufReader{Buf: data}, opts)
		_, _ = ReadStringsFromBuf(&BufReader{Buf: data}, opts)
	})
}

func FuzzArrayRoundTripSlicesAllTypes(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 5, 6, 7})
	f.Add([]byte("repeated strings and numeric values"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 || len(data) > 256 {
			return
		}
		n := len(data)
		start := uint64(data[0]) % uint64(n+1)
		end := start + uint64(data[len(data)-1])%(uint64(n)-start+1)

		int8Values := makeInt8Values(data)
		fuzzArraySlice(t, NewPrimitives(int8Values), int8Values, start, end, primitiveArrayReader[int8])
		int16Values := makeInt16Values(data)
		fuzzArraySlice(t, NewPrimitives(int16Values), int16Values, start, end, primitiveArrayReader[int16])
		int32Values := makeInt32Values(data)
		fuzzArraySlice(t, NewPrimitives(int32Values), int32Values, start, end, primitiveArrayReader[int32])
		int64Values := makeInt64Values(data)
		fuzzArraySlice(t, NewPrimitives(int64Values), int64Values, start, end, primitiveArrayReader[int64])
		uint8Values := makeUint8Values(data)
		fuzzArraySlice(t, NewPrimitives(uint8Values), uint8Values, start, end, primitiveArrayReader[uint8])
		uint16Values := makeUint16Values(data)
		fuzzArraySlice(t, NewPrimitives(uint16Values), uint16Values, start, end, primitiveArrayReader[uint16])
		uint32Values := makeUint32Values(data)
		fuzzArraySlice(t, NewPrimitives(uint32Values), uint32Values, start, end, primitiveArrayReader[uint32])
		uint64Values := makeUint64Values(data)
		fuzzArraySlice(t, NewPrimitives(uint64Values), uint64Values, start, end, primitiveArrayReader[uint64])
		float32Values := makeFloat32Values(data)
		fuzzArraySlice(t, NewPrimitives(float32Values), float32Values, start, end, primitiveArrayReader[float32])
		float64Values := makeFloat64Values(data)
		fuzzArraySlice(t, NewPrimitives(float64Values), float64Values, start, end, primitiveArrayReader[float64])

		strings := make([]string, n)
		for i := range strings {
			strings[i] = string([]byte{data[i]})
		}
		fuzzArraySlice(t, mustStrings(t, strings), strings, start, end, stringArrayReader)
	})
}

func fuzzArraySlice[T Integer | Float | String](t *testing.T, arr Array[T], values []T, start, end uint64, read arrayReader[T]) {
	t.Helper()

	var encoded bytes.Buffer
	if _, err := arr.WriteTo(&encoded); err != nil {
		t.Fatal(err)
	}
	loaded, err := read(&BufReader{Buf: encoded.Bytes()})
	if err != nil {
		t.Fatal(err)
	}
	sliced, err := loaded.Slice(start, end)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]T, sliced.Length())
	sliced.CopyTo(got)
	want := values[start:end]
	if !equalFuzzValues(want, got) {
		t.Fatalf("slice mismatch: got %v want %v", got, want)
	}

	var sliceBytes bytes.Buffer
	if _, err := sliced.WriteTo(&sliceBytes); err != nil {
		t.Fatal(err)
	}
	serialized, err := read(&BufReader{Buf: sliceBytes.Bytes()})
	if err != nil {
		t.Fatal(err)
	}
	got = make([]T, serialized.Length())
	serialized.CopyTo(got)
	if !equalFuzzValues(want, got) {
		t.Fatalf("serialized slice mismatch: got %v want %v", got, want)
	}
}

func equalFuzzValues[T Integer | Float | String](want, got []T) bool {
	if len(want) != len(got) {
		return false
	}
	for i := range want {
		if want[i] != got[i] && !(want[i] != want[i] && got[i] != got[i]) {
			return false
		}
	}
	return true
}

func makeInt8Values(data []byte) []int8 {
	values := make([]int8, len(data))
	for i, value := range data {
		values[i] = int8(value)
	}
	return values
}

func makeInt16Values(data []byte) []int16 {
	values := make([]int16, len(data))
	for i := range values {
		values[i] = int16(binary.LittleEndian.Uint16(repeatedBytes(data, i, 2)))
	}
	return values
}

func makeInt32Values(data []byte) []int32 {
	values := make([]int32, len(data))
	for i := range values {
		values[i] = int32(binary.LittleEndian.Uint32(repeatedBytes(data, i, 4)))
	}
	return values
}

func makeInt64Values(data []byte) []int64 {
	values := make([]int64, len(data))
	for i := range values {
		values[i] = int64(binary.LittleEndian.Uint64(repeatedBytes(data, i, 8)))
	}
	return values
}

func makeUint8Values(data []byte) []uint8 { return append([]uint8(nil), data...) }

func makeUint16Values(data []byte) []uint16 {
	values := make([]uint16, len(data))
	for i := range values {
		values[i] = binary.LittleEndian.Uint16(repeatedBytes(data, i, 2))
	}
	return values
}

func makeUint32Values(data []byte) []uint32 {
	values := make([]uint32, len(data))
	for i := range values {
		values[i] = binary.LittleEndian.Uint32(repeatedBytes(data, i, 4))
	}
	return values
}

func makeUint64Values(data []byte) []uint64 {
	values := make([]uint64, len(data))
	for i := range values {
		values[i] = binary.LittleEndian.Uint64(repeatedBytes(data, i, 8))
	}
	return values
}

func makeFloat32Values(data []byte) []float32 {
	values := make([]float32, len(data))
	for i := range values {
		values[i] = float32(int8(data[i])) / 3
	}
	return values
}

func makeFloat64Values(data []byte) []float64 {
	values := make([]float64, len(data))
	for i := range values {
		values[i] = float64(int8(data[i])) / 7
	}
	return values
}

func repeatedBytes(data []byte, index, width int) []byte {
	var result [8]byte
	for i := range width {
		result[i] = data[(index+i)%len(data)]
	}
	return result[:width]
}
