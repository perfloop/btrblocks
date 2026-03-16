package btrblocks

import (
	"encoding/binary"
	"fmt"
	"io"
	"unsafe"

	"github.com/axiomhq/btrblocks/array"
)

type FoRCodec[T UnsignedInteger] struct {
	min  T
	data Codec[T] // compressed residuals (value - min)
}

// forEncodedArray is a lazy array adapter that subtracts min from each element.
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
	return primitiveArrayHeaderSize + a.length*uint64(pTypeForType[T]().ByteWidth())
}

func (a forEncodedArray[T]) Length() uint64 { return a.length }
func (a forEncodedArray[T]) PType() PType   { return pTypeForType[T]() }

func (a forEncodedArray[T]) WriteTo(w io.Writer) (int64, error) {
	bodySize := a.length * uint64(pTypeForType[T]().ByteWidth())
	n, err := array.Header{
		Version:  1,
		PType:    array.PTypeForType[T](),
		Length:   a.length,
		BodySize: bodySize,
	}.WriteTo(w)
	if err != nil {
		return n, err
	}
	if a.length == 0 {
		return n, nil
	}

	const chunkElems = 1024
	var (
		buf     = make([]T, chunkElems)
		width   = int(unsafe.Sizeof(T(0)))
		written int64
	)
	for offset := uint64(0); offset < a.length; {
		chunk := len(buf)
		remaining := a.length - offset
		if remaining < uint64(chunk) {
			chunk = int(remaining)
		}
		for i := 0; i < chunk; i++ {
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

func NewFoRCodec[T UnsignedInteger](arr array.Array[T], depth int, excludes codecExcludes) (Codec[T], error) {
	if depth <= 0 {
		return nil, errDepthExhausted
	}
	if arr.Length() == 0 {
		return nil, errDataEmpty
	}

	min := arr.ValueAt(0)
	for i := uint64(1); i < arr.Length(); i++ {
		if v := arr.ValueAt(i); v < min {
			min = v
		}
	}

	childExcl := excludes.with(CodecTypeFoR)
	inner := CompressUnsignedInteger(forEncodedArray[T]{
		length: arr.Length(), min: min, valueAt: arr.ValueAt,
	}, depth-1, childExcl)
	return &FoRCodec[T]{min: min, data: inner}, nil
}

func (f *FoRCodec[T]) ValueAt(offset uint64) (T, error) {
	v, err := f.data.ValueAt(offset)
	if err != nil {
		return 0, err
	}
	return v + f.min, nil
}

func (f *FoRCodec[T]) Decode(dst []T) error {
	if err := validateDecodeLength(f.data.Length(), len(dst)); err != nil {
		return err
	}
	if err := f.data.Decode(dst); err != nil {
		return err
	}
	for i := range dst {
		dst[i] += f.min
	}
	return nil
}

func (f *FoRCodec[T]) Children() []Scheme { return []Scheme{f.data} }
func (f *FoRCodec[T]) Length() uint64     { return f.data.Length() }
func (f *FoRCodec[T]) PType() PType       { return pTypeForType[T]() }

func (f *FoRCodec[T]) BinarySize() uint64 {
	return uint64(headerSize) + uint64(unsafe.Sizeof(f.min)) + f.data.BinarySize()
}

func (f *FoRCodec[T]) WriteTo(w io.Writer) (n int64, err error) {
	minSize := uint64(unsafe.Sizeof(f.min))
	n, err = Header{
		Version:    1,
		Kind:       CodecTypeFoR,
		ElemType:   pTypeForType[T](),
		ChildCount: 1,
		Flags:      0,
		Length:     f.data.Length(),
		BodySize:   minSize,
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

	nn, err := f.data.WriteTo(w)
	return n + int64(nn), err
}

func readAnyFoRCodec[T Integer | Float | String](r io.Reader, header Header) (Codec[T], error) {
	var zero T
	switch any(zero).(type) {
	case uint8:
		c, err := readFoRCodec[uint8](r, header)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	case uint16:
		c, err := readFoRCodec[uint16](r, header)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	case uint32:
		c, err := readFoRCodec[uint32](r, header)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	case uint64:
		c, err := readFoRCodec[uint64](r, header)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	default:
		return nil, fmt.Errorf("codec: FoR not supported for type %v", header.ElemType)
	}
}

func readFoRCodec[T UnsignedInteger](r io.Reader, header Header) (Codec[T], error) {
	if header.ChildCount != 1 {
		return nil, fmt.Errorf("codec: FoR child count = %d, want 1", header.ChildCount)
	}
	minSize := uint64(unsafe.Sizeof(T(0)))
	if header.BodySize != minSize {
		return nil, fmt.Errorf("codec: FoR body size = %d, want %d", header.BodySize, minSize)
	}

	var buf [8]byte
	if _, err := io.ReadFull(r, buf[:minSize]); err != nil {
		return nil, err
	}
	var min T
	switch unsafe.Sizeof(min) {
	case 1:
		min = T(buf[0])
	case 2:
		min = T(binary.LittleEndian.Uint16(buf[:2]))
	case 4:
		min = T(binary.LittleEndian.Uint32(buf[:4]))
	case 8:
		min = T(binary.LittleEndian.Uint64(buf[:8]))
	}

	data, err := readCodec[T](r)
	if err != nil {
		return nil, err
	}
	if header.Length != data.Length() {
		return nil, fmt.Errorf("codec: FoR length = %d, want %d", header.Length, data.Length())
	}
	return &FoRCodec[T]{min: min, data: data}, nil
}
