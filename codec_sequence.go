package btrblocks

import (
	"errors"
	"fmt"
	"io"
	"unsafe"

	"github.com/axiomhq/btrblocks/array"
)

var errNotArithmeticSequence = errors.New("not an arithmetic sequence")

// sequenceArray stores an arithmetic progression as a base value and step.
type sequenceArray[T Integer] struct {
	length uint64
	base   T
	step   T
}

func newSequenceArray[T Integer](arr array.ArrayCore[T]) (*sequenceArray[T], error) {
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
	return &sequenceArray[T]{length: arr.Length(), base: base, step: step}, nil
}

func (s *sequenceArray[T]) Encoding() CodeType { return CodecTypeSequence }
func (s *sequenceArray[T]) Length() uint64     { return s.length }
func (s *sequenceArray[T]) PType() PType       { return array.PTypeForType[T]() }

func (s *sequenceArray[T]) BinarySize() uint64 {
	return uint64(headerSize) + 2*uint64(unsafe.Sizeof(s.base))
}

func (s *sequenceArray[T]) ValueAt(offset uint64) T {
	if offset >= s.length {
		panic(errOffsetOutOfRange)
	}
	return s.base + T(offset)*s.step
}

func (s *sequenceArray[T]) DecompressInto(dst []T) error {
	if err := checkDstLen(dst, s.length); err != nil {
		return err
	}
	for i := uint64(0); i < s.length; i++ {
		dst[i] = s.base + T(i)*s.step
	}
	return nil
}

func (s *sequenceArray[T]) Slice(start, end uint64) (EncodedArray[T], error) {
	if err := array.ValidateSliceBounds(s.length, start, end); err != nil {
		return nil, err
	}
	return &sequenceArray[T]{
		length: end - start,
		base:   s.base + T(start)*s.step,
		step:   s.step,
	}, nil
}

func (s *sequenceArray[T]) WriteTo(w io.Writer) (int64, error) {
	bodySize := 2 * uint64(unsafe.Sizeof(s.base))
	n, err := codecHeader{
		Version:  versionNumber,
		Kind:     CodecTypeSequence,
		ElemType: array.PTypeForType[T](),
		Length:   s.length,
		NumBytes: bodySize,
	}.WriteTo(w)
	if err != nil {
		return n, err
	}

	for _, value := range [2]T{s.base, s.step} {
		nn, err := writeIntegerLE(w, value)
		n += nn
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

func readAnySequenceArray[T Integer | Float | String](br *array.BufReader, h codecHeader, opts ReadOptions) (EncodedArray[T], error) {
	var zero T
	switch any(zero).(type) {
	case int8:
		return readCast[T](readSequenceArray[int8](br, h, opts))
	case int16:
		return readCast[T](readSequenceArray[int16](br, h, opts))
	case int32:
		return readCast[T](readSequenceArray[int32](br, h, opts))
	case int64:
		return readCast[T](readSequenceArray[int64](br, h, opts))
	case uint8:
		return readCast[T](readSequenceArray[uint8](br, h, opts))
	case uint16:
		return readCast[T](readSequenceArray[uint16](br, h, opts))
	case uint32:
		return readCast[T](readSequenceArray[uint32](br, h, opts))
	case uint64:
		return readCast[T](readSequenceArray[uint64](br, h, opts))
	default:
		return nil, fmt.Errorf("codec: sequence not supported for %v", h.ElemType)
	}
}

func readSequenceArray[T Integer](br *array.BufReader, h codecHeader, _ ReadOptions) (EncodedArray[T], error) {
	elemSize := uint64(unsafe.Sizeof(T(0)))
	if h.NumBytes != 2*elemSize {
		return nil, fmt.Errorf("codec: sequence body size = %d, want %d", h.NumBytes, 2*elemSize)
	}

	var vals [2]T
	for i := range vals {
		data, err := br.Read(int(elemSize))
		if err != nil {
			return nil, err
		}
		vals[i] = readIntegerLE[T](data)
	}
	return &sequenceArray[T]{length: h.Length, base: vals[0], step: vals[1]}, nil
}

func estimateSequence[T Integer](arr array.ArrayCore[T], _ planContext) (float64, bool) {
	codec, err := newSequenceArray(arr)
	if err != nil {
		return 0, false
	}
	return float64(rawBinarySize[T](arr)) / float64(codec.BinarySize()), true
}
