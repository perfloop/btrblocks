package codec

import (
	"fmt"
	"io"
	"unsafe"

	"github.com/axiomhq/btrblocks/array"
)

var errNotArithmeticSequence = ErrNotArithmeticSequence

// sequenceArray stores an arithmetic progression as a base value and step.
type sequenceArray[T Integer] struct {
	encodedNode
	denseRows
	base T
	step T
}

func newSequenceArray[T Integer](arr array.ArrayCore[T]) (*sequenceArray[T], error) {
	if arr.Length() == 0 {
		return nil, errDataEmpty
	}
	if arr.NullCount() != 0 {
		return nil, errNotArithmeticSequence
	}
	n := arr.Length()
	if n < 2 {
		return nil, errNotArithmeticSequence
	}
	base := arr.ValueAt(0)
	step := arr.ValueAt(1) - base
	if step == 0 {
		return nil, errNotArithmeticSequence
	}
	for i := uint64(2); i < n; i++ {
		if arr.ValueAt(i)-arr.ValueAt(i-1) != step {
			return nil, errNotArithmeticSequence
		}
	}
	return &sequenceArray[T]{denseRows: denseRows(n), base: base, step: step}, nil
}

// encodeSequence builds an arithmetic-sequence node.
func encodeSequence[T Integer](arr array.ArrayCore[T]) (EncodedArray[T], error) {
	return newSequenceArray(arr)
}

func (s *sequenceArray[T]) CodecType() CodecType {
	return CodecTypeSequence
}
func (s *sequenceArray[T]) PType() PType { return array.PTypeOfPrimitive[T]() }
func (s *sequenceArray[T]) DecodedBytes() (uint64, error) {
	return decodedBytesFor(s.Length(), s.PType())
}

func (s *sequenceArray[T]) BinarySize() uint64 {
	return uint64(headerSize) + 2*uint64(unsafe.Sizeof(s.base))
}

func (s *sequenceArray[T]) MarshalBinary() ([]byte, error) { return marshalBinary(s) }

func (s *sequenceArray[T]) ValueAt(offset uint64) T {
	if offset >= s.Length() {
		panic(errOffsetOutOfRange)
	}
	return s.base + T(offset)*s.step
}

func (s *sequenceArray[T]) DecompressInto(dst []T) error {
	if err := checkDstLen(dst, s.Length()); err != nil {
		return err
	}
	for i := range s.Length() {
		dst[i] = s.base + T(i)*s.step
	}
	return nil
}

func (s *sequenceArray[T]) Slice(start, end uint64) (EncodedArray[T], error) {
	if err := array.ValidateSliceBounds(s.Length(), start, end); err != nil {
		return nil, err
	}
	return &sequenceArray[T]{
		denseRows: denseRows(end - start),
		base:      s.base + T(start)*s.step,
		step:      s.step,
	}, nil
}

func (s *sequenceArray[T]) WriteTo(w io.Writer) (int64, error) {
	bodySize := 2 * uint64(unsafe.Sizeof(s.base))
	sum := writeSum{w: w}
	if err := sum.writeTo(codecHeader{
		Version:  versionNumber,
		Type:     CodecTypeSequence,
		ElemType: array.PTypeOfPrimitive[T](),
		Length:   s.Length(),
		NumBytes: bodySize,
	}); err != nil {
		return sum.n, err
	}
	for _, value := range [2]T{s.base, s.step} {
		if err := sum.add(writeIntegerLE(sum.w, value)); err != nil {
			return sum.n, err
		}
	}
	return sum.n, nil
}

func readSequenceArray[T Integer](br *array.BufReader, h codecHeader, _ *readOptions) (EncodedArray[T], error) {
	elemSize := uint64(unsafe.Sizeof(T(0)))
	if h.NumBytes != 2*elemSize {
		return nil, fmt.Errorf("codec: sequence body size = %d, want %d", h.NumBytes, 2*elemSize)
	}

	var vals [2]T
	for i := range vals {
		data, err := br.Read(int(elemSize))
		if err != nil {
			field := "base"
			if i == 1 {
				field = "step"
			}
			return nil, fmt.Errorf("codec: reading sequence %s: %w", field, err)
		}
		vals[i] = readIntegerLE[T](data)
	}
	return &sequenceArray[T]{denseRows: denseRows(h.Length), base: vals[0], step: vals[1]}, nil
}
