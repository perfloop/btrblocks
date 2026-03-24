package btrblocks

import (
	"encoding/binary"
	"fmt"
	"io"
	"unsafe"

	"github.com/axiomhq/btrblocks/array"
)

// forArray stores a reference value plus a child encoded array for biased deltas.
type forArray[T UnsignedInteger] struct {
	min   T
	child EncodedArray[T]
}

// forEncodedArray exposes values shifted by the shared minimum for child encoding.
type forEncodedArray[T UnsignedInteger] struct {
	length  uint64
	min     T
	valueAt func(uint64) T
}

func (a forEncodedArray[T]) ValueAt(offset uint64) T {
	return a.valueAt(offset) - a.min
}

func (a forEncodedArray[T]) CopyTo(dst []T) {
	for i := range dst {
		dst[i] = a.valueAt(uint64(i)) - a.min
	}
}

func (a forEncodedArray[T]) BinarySize() uint64 {
	return array.HeaderSize + a.length*uint64(array.PTypeForType[T]().ByteWidth())
}

func (a forEncodedArray[T]) Length() uint64 { return a.length }
func (a forEncodedArray[T]) PType() PType   { return array.PTypeForType[T]() }

func (a forEncodedArray[T]) Slice(start, end uint64) (array.Array[T], error) {
	return materializeSlice(a, start, end)
}

func (a forEncodedArray[T]) WriteTo(w io.Writer) (int64, error) {
	bodySize := a.length * uint64(array.PTypeForType[T]().ByteWidth())
	n, err := array.Header{
		Version: versionNumber,
		PType:   array.PTypeForType[T](),
		Length:  a.length,
		NBytes:  bodySize,
	}.WriteTo(w)
	if err != nil {
		return n, err
	}
	if a.length == 0 {
		return n, nil
	}

	const chunkElems = 1024
	buf := make([]T, chunkElems)
	width := int(unsafe.Sizeof(T(0)))
	var written int64
	for offset := uint64(0); offset < a.length; {
		chunk := len(buf)
		if remaining := a.length - offset; remaining < uint64(chunk) {
			chunk = int(remaining)
		}
		for i := range chunk {
			buf[i] = a.valueAt(offset+uint64(i)) - a.min
		}
		bytes := unsafe.Slice((*byte)(unsafe.Pointer(&buf[0])), chunk*width)
		wn, err := w.Write(bytes)
		written += int64(wn)
		if err != nil {
			return n + written, err
		}
		if wn != len(bytes) {
			return n + written, io.ErrShortWrite
		}
		offset += uint64(chunk)
	}
	return n + written, nil
}

func (f *forArray[T]) Encoding() CodeType { return CodecTypeFor }
func (f *forArray[T]) Length() uint64     { return f.child.Length() }
func (f *forArray[T]) PType() PType       { return array.PTypeForType[T]() }

func (f *forArray[T]) BinarySize() uint64 {
	return uint64(headerSize) + uint64(unsafe.Sizeof(f.min)) + f.child.BinarySize()
}

func (f *forArray[T]) ValueAt(offset uint64) T {
	return f.child.ValueAt(offset) + f.min
}

func (f *forArray[T]) DecompressInto(dst []T) error {
	if err := f.child.DecompressInto(dst); err != nil {
		return err
	}
	for i := uint64(0); i < f.child.Length(); i++ {
		dst[i] += f.min
	}
	return nil
}

func (f *forArray[T]) Decompress() ([]T, error) {
	dst := make([]T, f.child.Length())
	return dst, f.DecompressInto(dst)
}

func (f *forArray[T]) Slice(start, end uint64) (EncodedArray[T], error) {
	child, err := f.child.Slice(start, end)
	if err != nil {
		return nil, err
	}
	return &forArray[T]{min: f.min, child: child}, nil
}

func (f *forArray[T]) WriteTo(w io.Writer) (int64, error) {
	minSize := uint64(unsafe.Sizeof(f.min))
	n, err := header{
		Version:  versionNumber,
		Kind:     CodecTypeFor,
		ElemType: array.PTypeForType[T](),
		Length:   f.child.Length(),
		BodySize: minSize,
	}.WriteTo(w)
	if err != nil {
		return n, err
	}

	var buf [8]byte
	switch unsafe.Sizeof(f.min) {
	case 1:
		buf[0] = byte(f.min)
		nn, err := w.Write(buf[:1])
		n += int64(nn)
		if err != nil {
			return n, err
		}
	case 2:
		binary.LittleEndian.PutUint16(buf[:2], uint16(f.min))
		nn, err := w.Write(buf[:2])
		n += int64(nn)
		if err != nil {
			return n, err
		}
	case 4:
		binary.LittleEndian.PutUint32(buf[:4], uint32(f.min))
		nn, err := w.Write(buf[:4])
		n += int64(nn)
		if err != nil {
			return n, err
		}
	case 8:
		binary.LittleEndian.PutUint64(buf[:8], uint64(f.min))
		nn, err := w.Write(buf[:8])
		n += int64(nn)
		if err != nil {
			return n, err
		}
	}

	nn, err := f.child.WriteTo(w)
	return n + nn, err
}

func readAnyFoRArray[T Integer | Float | String](r io.Reader, h header) (EncodedArray[T], error) {
	var zero T
	switch any(zero).(type) {
	case uint8:
		c, err := readFoRArray[uint8](r, h)
		if err != nil {
			return nil, err
		}
		return any(c).(EncodedArray[T]), nil
	case uint16:
		c, err := readFoRArray[uint16](r, h)
		if err != nil {
			return nil, err
		}
		return any(c).(EncodedArray[T]), nil
	case uint32:
		c, err := readFoRArray[uint32](r, h)
		if err != nil {
			return nil, err
		}
		return any(c).(EncodedArray[T]), nil
	case uint64:
		c, err := readFoRArray[uint64](r, h)
		if err != nil {
			return nil, err
		}
		return any(c).(EncodedArray[T]), nil
	default:
		return nil, fmt.Errorf("codec: for not supported for %v", h.ElemType)
	}
}

func readFoRArray[T UnsignedInteger](r io.Reader, h header) (EncodedArray[T], error) {
	minSize := uint64(unsafe.Sizeof(T(0)))
	if h.BodySize != minSize {
		return nil, fmt.Errorf("codec: for body size = %d, want %d", h.BodySize, minSize)
	}

	var buf [8]byte
	if _, err := io.ReadFull(r, buf[:minSize]); err != nil {
		return nil, err
	}
	var minValue T
	switch unsafe.Sizeof(T(0)) {
	case 1:
		minValue = T(buf[0])
	case 2:
		minValue = T(binary.LittleEndian.Uint16(buf[:2]))
	case 4:
		minValue = T(binary.LittleEndian.Uint32(buf[:4]))
	case 8:
		minValue = T(binary.LittleEndian.Uint64(buf[:8]))
	}

	child, err := readEncodedArray[T](r)
	if err != nil {
		return nil, err
	}
	if child.Length() != h.Length {
		return nil, fmt.Errorf("codec: for length = %d, want %d", h.Length, child.Length())
	}
	return &forArray[T]{min: minValue, child: child}, nil
}

func buildFoRArray[T UnsignedInteger](arr array.Array[T], ctx planContext) (EncodedArray[T], error) {
	if ctx.depth <= 0 {
		return nil, errDepthExhausted
	}
	if arr.Length() == 0 {
		return nil, errDataEmpty
	}
	minValue := arr.ValueAt(0)
	for i := uint64(1); i < arr.Length(); i++ {
		if value := arr.ValueAt(i); value < minValue {
			minValue = value
		}
	}

	rangeWidth := bitWidthForUnsigned(uint64(arr.ValueAt(0) - minValue))
	for i := uint64(1); i < arr.Length(); i++ {
		if width := bitWidthForUnsigned(uint64(arr.ValueAt(i) - minValue)); width > rangeWidth {
			rangeWidth = width
		}
	}

	child, err := buildBitPackedArray(forEncodedArray[T]{
		length:  arr.Length(),
		min:     minValue,
		valueAt: arr.ValueAt,
	}, ctx.descend())
	if err != nil {
		return nil, err
	}
	bitpackChild, ok := child.(*bitPackedArray[T])
	if !ok {
		return nil, fmt.Errorf("codec: FoR child kind = %s, want bitpack", child.Encoding())
	}
	if bitpackChild.bitWidth > rangeWidth {
		return nil, fmt.Errorf("codec: FoR child bit width = %d, want <= %d", bitpackChild.bitWidth, rangeWidth)
	}
	return &forArray[T]{min: minValue, child: bitpackChild}, nil
}

func estimateFoR[T UnsignedInteger, S statsSource[T]](minValue, maxValue T) func(S, planContext) (float64, bool) {
	return func(stats S, ctx planContext) (float64, bool) {
		if ctx.depth <= 0 || minValue == 0 {
			return 0, false
		}

		bitpackWidth := bitWidthForUnsigned(uint64(maxValue))
		rangeWidth := bitWidthForUnsigned(uint64(maxValue - minValue))
		if rangeWidth == 0 || rangeWidth >= bitpackWidth {
			return 0, false
		}

		childSize, ok := bitpackEncodedSize(stats.Source().Length(), rangeWidth)
		if !ok {
			return 0, false
		}

		after := uint64(headerSize) + uint64(unsafe.Sizeof(minValue)) + childSize
		before := newRawArray(stats.Source()).BinarySize()
		if after >= before {
			return 0, false
		}
		return float64(before) / float64(after), true
	}
}
