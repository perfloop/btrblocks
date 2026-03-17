package btrblocks

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"unsafe"

	"github.com/axiomhq/btrblocks/array"
)

var errNotArithmeticSequence = errors.New("not an arithmetic sequence")

// SequenceCodec stores a perfect arithmetic sequence A[i] = base + i*step
// using only two scalars. No children needed — purely mathematical.
type SequenceCodec[T Integer] struct {
	length uint64
	base   T
	step   T
}

func NewSequenceCodec[T Integer](arr array.Array[T], depth int, excludes codecExcludes) (Codec[T], error) {
	if arr.Length() == 0 {
		return nil, errDataEmpty
	}
	if depth <= 0 {
		return nil, errDepthExhausted
	}
	if arr.Length() < 2 {
		return nil, errNotArithmeticSequence
	}

	base := arr.ValueAt(0)
	step := arr.ValueAt(1) - base

	if step == 0 {
		return nil, errNotArithmeticSequence
	}

	for i := uint64(2); i < arr.Length(); i++ {
		if arr.ValueAt(i)-arr.ValueAt(i-1) != step {
			return nil, errNotArithmeticSequence
		}
	}

	return &SequenceCodec[T]{length: arr.Length(), base: base, step: step}, nil
}

func (s *SequenceCodec[T]) ValueAt(offset uint64) (T, error) {
	if offset >= s.length {
		var zero T
		return zero, errOffsetOutOfRange
	}
	return s.base + T(offset)*s.step, nil
}

func (s *SequenceCodec[T]) Decode(dst []T) error {
	if err := validateDecodeLength(s.length, len(dst)); err != nil {
		return err
	}
	for i := range dst {
		dst[i] = s.base + T(i)*s.step
	}
	return nil
}

func (s *SequenceCodec[T]) Children() []Scheme { return nil }
func (s *SequenceCodec[T]) Length() uint64     { return s.length }
func (s *SequenceCodec[T]) PType() PType       { return pTypeForType[T]() }

func (s *SequenceCodec[T]) BinarySize() uint64 {
	return uint64(headerSize) + 2*uint64(unsafe.Sizeof(s.base))
}

func (s *SequenceCodec[T]) WriteTo(w io.Writer) (n int64, err error) {
	bodySize := 2 * uint64(unsafe.Sizeof(s.base))
	n, err = Header{
		Version:    1,
		Kind:       CodecTypeSequence,
		ElemType:   pTypeForType[T](),
		ChildCount: 0,
		Flags:      0,
		Length:     s.length,
		BodySize:   bodySize,
	}.WriteTo(w)
	if err != nil {
		return n, err
	}

	var buf [8]byte
	width := unsafe.Sizeof(s.base)

	for _, val := range [2]T{s.base, s.step} {
		switch width {
		case 1:
			buf[0] = byte(val)
			nn, werr := w.Write(buf[:1])
			n += int64(nn)
			if werr != nil {
				return n, werr
			}
		case 2:
			binary.LittleEndian.PutUint16(buf[:2], uint16(val))
			nn, werr := w.Write(buf[:2])
			n += int64(nn)
			if werr != nil {
				return n, werr
			}
		case 4:
			binary.LittleEndian.PutUint32(buf[:4], uint32(val))
			nn, werr := w.Write(buf[:4])
			n += int64(nn)
			if werr != nil {
				return n, werr
			}
		case 8:
			binary.LittleEndian.PutUint64(buf[:8], uint64(val))
			nn, werr := w.Write(buf[:8])
			n += int64(nn)
			if werr != nil {
				return n, werr
			}
		}
	}

	return n, nil
}

func readAnySequenceCodec[T Integer | Float | String](r io.Reader, header Header) (Codec[T], error) {
	var zero T
	switch any(zero).(type) {
	case int8:
		c, err := readSequenceCodec[int8](r, header)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	case int16:
		c, err := readSequenceCodec[int16](r, header)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	case int32:
		c, err := readSequenceCodec[int32](r, header)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	case int64:
		c, err := readSequenceCodec[int64](r, header)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	case uint8:
		c, err := readSequenceCodec[uint8](r, header)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	case uint16:
		c, err := readSequenceCodec[uint16](r, header)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	case uint32:
		c, err := readSequenceCodec[uint32](r, header)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	case uint64:
		c, err := readSequenceCodec[uint64](r, header)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	default:
		return nil, fmt.Errorf("codec: Sequence not supported for type %v", header.ElemType)
	}
}

func readSequenceCodec[T Integer](r io.Reader, header Header) (Codec[T], error) {
	if header.ChildCount != 0 {
		return nil, fmt.Errorf("codec: Sequence child count = %d, want 0", header.ChildCount)
	}
	elemSize := uint64(unsafe.Sizeof(T(0)))
	if header.BodySize != 2*elemSize {
		return nil, fmt.Errorf("codec: Sequence body size = %d, want %d", header.BodySize, 2*elemSize)
	}

	var buf [8]byte
	var vals [2]T
	for i := range vals {
		if _, err := io.ReadFull(r, buf[:elemSize]); err != nil {
			return nil, err
		}
		switch unsafe.Sizeof(T(0)) {
		case 1:
			vals[i] = T(buf[0])
		case 2:
			vals[i] = T(binary.LittleEndian.Uint16(buf[:2]))
		case 4:
			vals[i] = T(binary.LittleEndian.Uint32(buf[:4]))
		case 8:
			vals[i] = T(binary.LittleEndian.Uint64(buf[:8]))
		}
	}

	return &SequenceCodec[T]{length: header.Length, base: vals[0], step: vals[1]}, nil
}
