package codec

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"testing"
	"unsafe"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

func buildArray[T Integer | Float](data []T) array.Array[T] {
	return array.NewPrimitivesUnsafe(data)
}

func testRawChild[T Integer | Float](child array.ArrayCore[T]) (EncodedArray[T], error) {
	values := make([]T, child.Length())
	for i := range values {
		values[i] = child.ValueAt(uint64(i))
	}
	return encodeRaw(array.NewPrimitivesUnsafe(values)), nil
}

func testRawStringChild(child array.ArrayCore[string]) (EncodedArray[string], error) {
	values := make([]string, child.Length())
	for i := range values {
		values[i] = child.ValueAt(uint64(i))
	}
	arr, err := array.NewStrings(values)
	if err != nil {
		return nil, err
	}
	return encodeRaw(arr), nil
}

type testUnsignedChildren struct{}

var testBuildBudget = buildBudget{maxBytes: DefaultMaxBuildBytes}

func (testUnsignedChildren) BuildUint8(child array.ArrayCore[uint8]) (EncodedArray[uint8], error) {
	return testRawChild(child)
}

func (testUnsignedChildren) BuildUint16(child array.ArrayCore[uint16]) (EncodedArray[uint16], error) {
	return testRawChild(child)
}

func (testUnsignedChildren) BuildUint32(child array.ArrayCore[uint32]) (EncodedArray[uint32], error) {
	return testRawChild(child)
}

func (testUnsignedChildren) BuildUint64(child array.ArrayCore[uint64]) (EncodedArray[uint64], error) {
	return testRawChild(child)
}

func TestRawRoundTrip(t *testing.T) {
	t.Run("uint32", func(t *testing.T) {
		values := []uint32{1, 2, 3, 100, 200, 300}
		assertRoundTrip(t, newRawArray(buildArray(values)), values)
	})
	t.Run("int32", func(t *testing.T) {
		values := []int32{-10, 0, 10, math.MinInt32, math.MaxInt32}
		assertRoundTrip(t, newRawArray(buildArray(values)), values)
	})
	t.Run("float64", func(t *testing.T) {
		values := []float64{0.0, -1.5, 3.14, math.Inf(1), math.NaN()}
		assertRoundTrip(t, newRawArray(buildArray(values)), values)
	})
	t.Run("string", func(t *testing.T) {
		values := []string{"hello", "", "world", "foo bar"}
		assertRoundTrip(t, newRawArray(mustStrings(t, values)), values)
	})
	t.Run("uint64", func(t *testing.T) {
		values := []uint64{0, 1, math.MaxUint64, 42}
		assertRoundTrip(t, newRawArray(buildArray(values)), values)
	})
}

func TestRawEncoding(t *testing.T) {
	codec := newRawArray(buildArray([]uint32{1, 2, 3}))
	require.Equal(t, CodecTypeRaw, codec.CodecType())
}

func TestRawSlice(t *testing.T) {
	t.Run("before serialization", func(t *testing.T) {
		values := []uint32{10, 20, 30, 40, 50}
		assertSliceRoundTrip(t, newRawArray(buildArray(values)), 1, 4, values)
	})
	t.Run("after deserialization", func(t *testing.T) {
		values := []int32{-3, -2, -1, 0, 1, 2, 3}
		data := mustWriteEncodedArray(t, newRawArray(buildArray(values)))
		loaded, err := LoadSigned[int32](data)
		require.NoError(t, err)
		assertSliceRoundTrip(t, loaded, 2, 5, values)
	})
}

func BenchmarkRaw(b *testing.B) {
	for _, n := range benchSizes {
		values := make([]uint32, n)
		for i := range values {
			values[i] = uint32(i)
		}
		arr := buildArray(values)

		b.Run(fmt.Sprintf("compress/%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				codec := newRawArray(arr)
				var buf bytes.Buffer
				_, _ = codec.WriteTo(&buf)
			}
		})

		b.Run(fmt.Sprintf("decompress/%d", n), func(b *testing.B) {
			data := mustWriteEncodedArray(b, newRawArray(arr))
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				readBack, err := LoadUnsigned[uint32](data)
				if err != nil {
					b.Fatal(err)
				}
				_, err = Decompress(readBack)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func FuzzRaw(f *testing.F) {
	f.Add([]byte{1, 0, 0, 0, 2, 0, 0, 0, 3, 0, 0, 0})
	f.Add(make([]byte, 4))
	f.Add([]byte{0xff, 0xff, 0xff, 0xff})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 4 || len(data)%4 != 0 {
			return
		}
		n := len(data) / 4
		values := unsafe.Slice((*uint32)(unsafe.Pointer(&data[0])), n)
		owned := make([]uint32, n)
		copy(owned, values)
		assertRoundTrip(t, newRawArray(buildArray(owned)), owned)
	})
}

func FuzzRawFloat64(f *testing.F) {
	var seed bytes.Buffer
	for _, v := range []float64{0.0, 1.5, -1.5, math.Inf(1)} {
		_ = binary.Write(&seed, binary.LittleEndian, v)
	}
	f.Add(seed.Bytes())

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 8 || len(data)%8 != 0 {
			return
		}
		n := len(data) / 8
		values := unsafe.Slice((*float64)(unsafe.Pointer(&data[0])), n)
		owned := make([]float64, n)
		copy(owned, values)
		assertRoundTrip(t, newRawArray(buildArray(owned)), owned)
	})
}
