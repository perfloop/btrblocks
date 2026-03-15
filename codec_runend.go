package btrblocks

import (
	"fmt"
	"io"

	"github.com/axiomhq/btrblocks/array"
)

type RunendCodec[T Integer | Float | String, U UnsignedInteger] struct {
	length uint64
	runs   Codec[T]
	ends   Codec[U]
}

func newRunendCodecWithWidth[T Integer | Float | String, U UnsignedInteger](length uint64, runs []T, ends []uint64, depth int) (Codec[T], error) {
	narrow := make([]U, len(ends))
	for i, end := range ends {
		narrow[i] = U(end)
	}
	runsCodec := compress(runs, depth-1)
	endsCodec := CompressInteger(array.NewPrimitivesUnsafe(narrow), depth-1)
	return &RunendCodec[T, U]{length: length, runs: runsCodec, ends: endsCodec}, nil
}

func newRunendCodec[T Integer | Float | String](data []T, cmpFn cmpFn[T], depth int) (Codec[T], error) {
	if len(data) == 0 {
		return nil, errDataEmpty
	}
	if depth <= 0 {
		return nil, errDepthExhausted
	}
	runs := make([]T, 0, len(data))
	ends := make([]uint64, 0, len(data))
	for i, val := range data {
		if i == 0 {
			runs = append(runs, val)
			continue
		}
		if !cmpFn(runs[len(runs)-1], val) {
			ends = append(ends, uint64(i))
			runs = append(runs, val)
		}
	}
	maxEnd := uint64(0)
	if len(ends) > 0 {
		maxEnd = ends[len(ends)-1]
	}
	switch {
	case maxEnd <= uint64(^uint8(0)):
		return newRunendCodecWithWidth[T, uint8](uint64(len(data)), runs, ends, depth)
	case maxEnd <= uint64(^uint16(0)):
		return newRunendCodecWithWidth[T, uint16](uint64(len(data)), runs, ends, depth)
	case maxEnd <= uint64(^uint32(0)):
		return newRunendCodecWithWidth[T, uint32](uint64(len(data)), runs, ends, depth)
	default:
		return newRunendCodecWithWidth[T, uint64](uint64(len(data)), runs, ends, depth)
	}
}

func NewRunendIntegerCodec[T Integer](data []T, depth int) (Codec[T], error) {
	return newRunendCodec(data, cmpIntegers[T], depth)
}

func NewRunendStringCodec[T String](data []T, depth int) (Codec[T], error) {
	return newRunendCodec(data, cmpStrings[T], depth)
}

func NewRunendFloatCodec[T Float](data []T, depth int) (Codec[T], error) {
	return newRunendCodec(data, cmpFloats[T], depth)
}

func (r *RunendCodec[T, U]) Decode(dst []T) error {
	runs := make([]T, r.runs.Length())
	if err := r.runs.Decode(runs); err != nil {
		return err
	}
	ends := make([]U, r.ends.Length())
	if err := r.ends.Decode(ends); err != nil {
		return err
	}
	pos := 0
	for i, run := range runs {
		var end int
		if i < len(ends) {
			end = int(ends[i])
		} else {
			end = len(dst)
		}
		for pos < end {
			dst[pos] = run
			pos++
		}
	}
	return nil
}

func (r *RunendCodec[T, U]) Children() []Scheme { return []Scheme{r.runs.(Scheme), r.ends.(Scheme)} }

func (r *RunendCodec[T, U]) ValueAt(offset uint64) (T, error) {
	var zero T
	if offset >= r.length {
		return zero, errOffsetOutOfRange
	}

	lo, hi := uint64(0), r.ends.Length()
	for lo < hi {
		mid := lo + (hi-lo)/2
		end, err := r.ends.ValueAt(mid)
		if err != nil {
			return zero, err
		}
		if offset < uint64(end) {
			hi = mid
			continue
		}
		lo = mid + 1
	}

	return r.runs.ValueAt(lo)
}

func (r *RunendCodec[T, U]) WriteTo(w io.Writer) (n int64, err error) {
	n, err = Header{
		Version:    1,
		Kind:       CodecTypeRunend,
		ElemType:   pTypeForType[T](),
		ChildCount: 2,
		Flags:      0,
		Length:     r.length,
		BodySize:   0,
	}.WriteTo(w)
	if err != nil {
		return n, err
	}

	nn, err := r.runs.WriteTo(w)
	if err != nil {
		return n + int64(nn), err
	}

	n += int64(nn)

	nn, err = r.ends.WriteTo(w)
	return n + int64(nn), err
}

func (r *RunendCodec[T, U]) BinarySize() uint64 {
	return uint64(headerSize) + r.runs.BinarySize() + r.ends.BinarySize()
}
func (r *RunendCodec[T, U]) Length() uint64 { return r.length }
func (r *RunendCodec[T, U]) PType() PType   { return pTypeForType[T]() }

func readRunendCodecWithEnds[T Integer | Float | String, U UnsignedInteger](r io.Reader, header Header, runs Codec[T], childHeader Header) (Codec[T], error) {
	ends, err := readCodecWithHeader[U](r, childHeader)
	if err != nil {
		return nil, err
	}
	if err := validateRunendCodec(header.Length, runs, ends); err != nil {
		return nil, err
	}
	return &RunendCodec[T, U]{length: header.Length, runs: runs, ends: ends}, nil
}

func validateRunendCodec[T Integer | Float | String, U UnsignedInteger](length uint64, runs Codec[T], ends Codec[U]) error {
	if runs.Length() == 0 {
		return fmt.Errorf("codec: runend runs length = 0")
	}
	if runs.Length() != ends.Length()+1 {
		return fmt.Errorf("codec: runend runs length = %d, want %d", runs.Length(), ends.Length()+1)
	}
	if runs.Length() > length {
		return fmt.Errorf("codec: runend runs length = %d exceeds length %d", runs.Length(), length)
	}
	var prev uint64
	for i := uint64(0); i < ends.Length(); i++ {
		end, err := ends.ValueAt(i)
		if err != nil {
			return err
		}
		value := uint64(end)
		if value == 0 || value >= length {
			return fmt.Errorf("codec: runend end %d = %d out of range for length %d", i, value, length)
		}
		if i > 0 && value <= prev {
			return fmt.Errorf("codec: runend ends must be strictly increasing")
		}
		prev = value
	}
	return nil
}

func readRunendCodec[T Integer | Float | String](r io.Reader, header Header) (Codec[T], error) {
	if header.ChildCount != 2 {
		return nil, fmt.Errorf("codec: runend child count = %d, want 2", header.ChildCount)
	}
	if header.BodySize != 0 {
		return nil, fmt.Errorf("codec: runend body size = %d, want 0", header.BodySize)
	}
	if header.Length == 0 {
		return nil, fmt.Errorf("codec: runend length = 0")
	}
	runs, err := readCodec[T](r)
	if err != nil {
		return nil, err
	}
	childHeader, err := readHeader(r)
	if err != nil {
		return nil, err
	}
	switch childHeader.ElemType {
	case PTypeUint8:
		return readRunendCodecWithEnds[T, uint8](r, header, runs, childHeader)
	case PTypeUint16:
		return readRunendCodecWithEnds[T, uint16](r, header, runs, childHeader)
	case PTypeUint32:
		return readRunendCodecWithEnds[T, uint32](r, header, runs, childHeader)
	case PTypeUint64:
		return readRunendCodecWithEnds[T, uint64](r, header, runs, childHeader)
	default:
		return nil, fmt.Errorf("codec: runend end element type = %v, want unsigned integer", childHeader.ElemType)
	}
}
