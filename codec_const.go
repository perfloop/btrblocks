package btrblocks

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"unsafe"

	"github.com/axiomhq/btrblocks/array"
)

var errValueNotConstant = errors.New("not constant")

// constArray stores one repeated value for the entire logical array length.
type constArray[T Integer | Float | String] struct {
	length uint64
	value  T
}

func (c *constArray[T]) Encoding() CodeType { return CodecTypeConst }
func (c *constArray[T]) Length() uint64     { return c.length }
func (c *constArray[T]) PType() PType       { return array.PTypeForType[T]() }
func (c *constArray[T]) BinarySize() uint64 { return uint64(headerSize) + constBodyBinarySize(c.value) }

func (c *constArray[T]) ValueAt(offset uint64) T {
	if offset >= c.length {
		panic(errOffsetOutOfRange)
	}
	return c.value
}

func (c *constArray[T]) DecompressInto(dst []T) error {
	if err := checkDstLen(dst, c.length); err != nil {
		return err
	}
	fillRun(dst, 0, int(c.length), c.value)
	return nil
}

func (c *constArray[T]) Slice(start, end uint64) (EncodedArray[T], error) {
	if err := array.ValidateSliceBounds(c.length, start, end); err != nil {
		return nil, err
	}
	return &constArray[T]{length: end - start, value: c.value}, nil
}

func (c *constArray[T]) WriteTo(w io.Writer) (int64, error) {
	bodySize := constBodyBinarySize(c.value)
	n, err := codecHeader{
		Version:  versionNumber,
		Kind:     CodecTypeConst,
		ElemType: array.PTypeForType[T](),
		Length:   c.length,
		NumBytes: bodySize,
	}.WriteTo(w)
	if err != nil {
		return n, err
	}
	nn, err := writeConstElementArray(w, c.value)
	return n + nn, err
}

func newConstArray[T Integer | Float | String](arr array.ArrayCore[T], cmp cmpFn[T]) (*constArray[T], error) {
	n := arr.Length()
	if n == 0 {
		return nil, errDataEmpty
	}
	value := arr.ValueAt(0)
	for i := uint64(1); i < n; i++ {
		if !cmp(value, arr.ValueAt(i)) {
			return nil, errValueNotConstant
		}
	}
	return &constArray[T]{length: n, value: value}, nil
}

func newConstIntegerArray[T Integer](arr array.ArrayCore[T]) (*constArray[T], error) {
	return newConstArray(arr, cmpIntegers[T])
}

func newConstFloatArray[T Float](arr array.ArrayCore[T]) (*constArray[T], error) {
	return newConstArray(arr, cmpFloatBits[T])
}

func newConstStringArray[T String](arr array.ArrayCore[T]) (*constArray[T], error) {
	return newConstArray(arr, cmpStrings[T])
}

func readConstArray[T Integer | Float | String](br *array.BufReader, h codecHeader, opts ReadOptions) (EncodedArray[T], error) {
	arr, err := array.ReadArrayFromBuf[T](br, opts)
	if err != nil {
		return nil, err
	}
	if h.Length == 0 {
		return nil, fmt.Errorf("codec: const length = 0")
	}
	if arr.Length() != 1 {
		return nil, fmt.Errorf("codec: const body length = %d, want 1", arr.Length())
	}
	if h.NumBytes != arr.BinarySize() {
		return nil, fmt.Errorf("codec: const body size = %d, want %d", h.NumBytes, arr.BinarySize())
	}
	return &constArray[T]{length: h.Length, value: arr.ValueAt(0)}, nil
}

func estimateConst[T Integer | Float | String](arr array.ArrayCore[T], ctx planContext, isConst bool) (float64, bool) {
	if ctx.isSample || !isConst {
		return 0, false
	}
	return float64(arr.Length()) + 1, true
}

func constBodyBinarySize[T Integer | Float | String](value T) uint64 {
	var zero T
	switch any(zero).(type) {
	case string:
		size := uint64(len(any(value).(string)))
		var offsetWidth uint64
		switch {
		case size <= uint64(^uint8(0)):
			offsetWidth = 1
		case size <= uint64(^uint16(0)):
			offsetWidth = 2
		default:
			offsetWidth = 4
		}
		return uint64(array.HeaderSize) + 4 + 2*offsetWidth + size
	default:
		return uint64(array.HeaderSize) + uint64(array.PTypeForType[T]().ByteWidth())
	}
}

// writeConstElementArray writes a single-element array body (header + value)
// directly to w without allocating an intermediate array.Array. For primitives,
// the in-memory bytes are the wire format (little-endian platform required).
// Strings fall back to array.NewStrings for offset/buffer construction.
func writeConstElementArray[T Integer | Float | String](w io.Writer, value T) (int64, error) {
	var zero T
	switch any(zero).(type) {
	case string:
		body := array.NewStrings([]string{any(value).(string)})
		return body.WriteTo(w)
	default:
		pt := array.PTypeForType[T]()
		width := pt.ByteWidth()
		n, err := array.Header{Version: versionNumber, PType: pt, Length: 1, NumBytes: uint64(width)}.WriteTo(w)
		if err != nil {
			return n, err
		}
		var buf [8]byte
		// Read the raw bits of value via unsafe, then write as explicit
		// little-endian. This is safe because the string case is handled
		// above — only numeric types reach here.
		bits := *(*uint64)(unsafe.Pointer(&value))
		switch width {
		case 1:
			buf[0] = byte(bits)
		case 2:
			binary.LittleEndian.PutUint16(buf[:2], uint16(bits))
		case 4:
			binary.LittleEndian.PutUint32(buf[:4], uint32(bits))
		case 8:
			binary.LittleEndian.PutUint64(buf[:8], bits)
		}
		nn, err := w.Write(buf[:width])
		if err == nil && nn != width {
			err = io.ErrShortWrite
		}
		return n + int64(nn), err
	}
}
