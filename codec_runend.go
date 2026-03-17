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

func newRunendCodecFromSource[T Integer | Float | String](length uint64, valueAt func(uint64) T, cmpFn cmpFn[T], depth int, excludes codecExcludes) (Codec[T], error) {
	if length == 0 {
		return nil, errDataEmpty
	}
	if depth <= 0 {
		return nil, errDepthExhausted
	}
	var (
		runs = make([]T, 0)
		ends = make([]uint64, 0)
		prev = valueAt(0)
	)
	runs = append(runs, prev)
	for i := uint64(1); i < length; i++ {
		val := valueAt(i)
		if !cmpFn(prev, val) {
			ends = append(ends, i)
			runs = append(runs, val)
			prev = val
		}
	}
	maxEnd := uint64(0)
	if len(ends) > 0 {
		maxEnd = ends[len(ends)-1]
	}
	switch {
	case maxEnd <= uint64(^uint8(0)):
		return newRunendCodecWithWidth[T, uint8](length, runs, ends, depth, excludes)
	case maxEnd <= uint64(^uint16(0)):
		return newRunendCodecWithWidth[T, uint16](length, runs, ends, depth, excludes)
	case maxEnd <= uint64(^uint32(0)):
		return newRunendCodecWithWidth[T, uint32](length, runs, ends, depth, excludes)
	default:
		return newRunendCodecWithWidth[T, uint64](length, runs, ends, depth, excludes)
	}
}

func newRunendCodecWithWidth[T Integer | Float | String, U UnsignedInteger](length uint64, runs []T, ends []uint64, depth int, excludes codecExcludes) (Codec[T], error) {
	narrow := make([]U, len(ends))
	for i, end := range ends {
		narrow[i] = U(end)
	}
	childExcl := excludes.with(CodecTypeRunend, CodecTypeDict)
	runsCodec := compress(runs, depth-1, childExcl)
	endsCodec := CompressInteger(array.NewPrimitivesUnsafe(narrow), depth-1, childExcl)
	return &RunendCodec[T, U]{length: length, runs: runsCodec, ends: endsCodec}, nil
}

func NewRunendIntegerCodec[T Integer](arr array.Array[T], depth int, excludes codecExcludes) (Codec[T], error) {
	return newRunendCodecFromSource(arr.Length(), arr.ValueAt, cmpIntegers[T], depth, excludes)
}

func NewRunendStringCodec[T String](arr array.Array[T], depth int, excludes codecExcludes) (Codec[T], error) {
	return newRunendCodecFromSource(arr.Length(), arr.ValueAt, cmpStrings[T], depth, excludes)
}

func NewRunendFloatCodec[T Float](arr array.Array[T], depth int, excludes codecExcludes) (Codec[T], error) {
	return newRunendCodecFromSource(arr.Length(), arr.ValueAt, cmpFloats[T], depth, excludes)
}

// fillRun writes value into dst[start:end] without any extra allocation.
// It seeds the first slot and then doubles the initialized prefix with copy,
// which is noticeably cheaper than one assignment per decoded value on long runs.
func fillRun[T Integer | Float | String](dst []T, start, end int, value T) {
	if start >= end {
		return
	}
	dst[start] = value
	filled := 1
	for start+filled < end {
		n := filled
		if start+filled+n > end {
			n = end - (start + filled)
		}
		copy(dst[start+filled:start+filled+n], dst[start:start+n])
		filled += n
	}
}

// Decode expands run-end encoded data into dst in a single forward pass.
//
// ends[i] is the exclusive upper bound for runs[i]. The final run does not
// store an end; it implicitly extends to Length().
func (r *RunendCodec[T, U]) Decode(dst []T) error {
	if err := validateDecodeLength(r.length, len(dst)); err != nil {
		return err
	}
	runs := make([]T, r.runs.Length())
	if err := r.runs.Decode(runs); err != nil {
		return err
	}
	ends := make([]U, r.ends.Length())
	if err := r.ends.Decode(ends); err != nil {
		return err
	}

	pos := 0
	for i, rawEnd := range ends {
		end := int(rawEnd)
		fillRun(dst, pos, end, runs[i])
		pos = end
	}
	fillRun(dst, pos, len(dst), runs[len(ends)])
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
	// O(1) structural checks only; per-element invariants are guaranteed by
	// the encoder (no per-element validation on decode).
	if runs.Length() == 0 {
		return nil, fmt.Errorf("codec: runend runs length = 0")
	}
	if runs.Length() != ends.Length()+1 {
		return nil, fmt.Errorf("codec: runend runs length = %d, want %d", runs.Length(), ends.Length()+1)
	}
	if runs.Length() > header.Length {
		return nil, fmt.Errorf("codec: runend runs length = %d exceeds length %d", runs.Length(), header.Length)
	}
	return &RunendCodec[T, U]{length: header.Length, runs: runs, ends: ends}, nil
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
