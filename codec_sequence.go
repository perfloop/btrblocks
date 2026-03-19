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

type sequenceCodec[T Integer] struct {
	length uint64
	base   T
	step   T
}

func newSequenceCodec[T Integer](arr array.Array[T]) (*sequenceCodec[T], error) {
	if arr.Length() == 0 {
		return nil, errDataEmpty
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
	return &sequenceCodec[T]{length: arr.Length(), base: base, step: step}, nil
}

func (s *sequenceCodec[T]) Kind() CodeType { return CodecTypeSequence }
func (s *sequenceCodec[T]) Length() uint64 { return s.length }
func (s *sequenceCodec[T]) PType() PType   { return pTypeForType[T]() }

func (s *sequenceCodec[T]) BinarySize() uint64 {
	return uint64(headerSize) + 2*uint64(unsafe.Sizeof(s.base))
}

func (s *sequenceCodec[T]) ValueAt(offset uint64) T {
	if offset >= s.length {
		panic(errOffsetOutOfRange)
	}
	return s.base + T(offset)*s.step
}

func (s *sequenceCodec[T]) Decode(dst []T) error {
	if err := validateDecodeLength(s.length, len(dst)); err != nil {
		return err
	}
	for i := range dst {
		dst[i] = s.base + T(i)*s.step
	}
	return nil
}

func (s *sequenceCodec[T]) WriteTo(w io.Writer) (int64, error) {
	bodySize := 2 * uint64(unsafe.Sizeof(s.base))
	n, err := header{
		Version:  versionNumber,
		Kind:     CodecTypeSequence,
		ElemType: pTypeForType[T](),
		Length:   s.length,
		BodySize: bodySize,
	}.WriteTo(w)
	if err != nil {
		return n, err
	}

	var buf [8]byte
	width := unsafe.Sizeof(s.base)
	for _, value := range [2]T{s.base, s.step} {
		switch width {
		case 1:
			buf[0] = byte(value)
			nn, err := w.Write(buf[:1])
			n += int64(nn)
			if err != nil {
				return n, err
			}
		case 2:
			binary.LittleEndian.PutUint16(buf[:2], uint16(value))
			nn, err := w.Write(buf[:2])
			n += int64(nn)
			if err != nil {
				return n, err
			}
		case 4:
			binary.LittleEndian.PutUint32(buf[:4], uint32(value))
			nn, err := w.Write(buf[:4])
			n += int64(nn)
			if err != nil {
				return n, err
			}
		case 8:
			binary.LittleEndian.PutUint64(buf[:8], uint64(value))
			nn, err := w.Write(buf[:8])
			n += int64(nn)
			if err != nil {
				return n, err
			}
		}
	}

	return n, nil
}

func readAnySequenceCodec[T Integer | Float | String](r io.Reader, h header) (Codec[T], error) {
	var zero T
	switch any(zero).(type) {
	case int8:
		c, err := readSequenceCodec[int8](r, h)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	case int16:
		c, err := readSequenceCodec[int16](r, h)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	case int32:
		c, err := readSequenceCodec[int32](r, h)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	case int64:
		c, err := readSequenceCodec[int64](r, h)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	case uint8:
		c, err := readSequenceCodec[uint8](r, h)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	case uint16:
		c, err := readSequenceCodec[uint16](r, h)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	case uint32:
		c, err := readSequenceCodec[uint32](r, h)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	case uint64:
		c, err := readSequenceCodec[uint64](r, h)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	default:
		return nil, fmt.Errorf("codec: sequence not supported for %v", h.ElemType)
	}
}

func readSequenceCodec[T Integer](r io.Reader, h header) (Codec[T], error) {
	elemSize := uint64(unsafe.Sizeof(T(0)))
	if h.BodySize != 2*elemSize {
		return nil, fmt.Errorf("codec: sequence body size = %d, want %d", h.BodySize, 2*elemSize)
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
	return &sequenceCodec[T]{length: h.Length, base: vals[0], step: vals[1]}, nil
}

func estimateSequence[T Integer](arr array.Array[T], _ planContext) (float64, bool) {
	codec, err := newSequenceCodec(arr)
	if err != nil {
		return 0, false
	}
	return float64(rawBinarySize(arr)) / float64(codec.BinarySize()), true
}
