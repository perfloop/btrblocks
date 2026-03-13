package btrblocks

import (
	"io"

	"github.com/axiomhq/btrblocks/array"
)

var (
	_ Codec[int8]    = (*RunendCodec[int8])(nil)
	_ Codec[int16]   = (*RunendCodec[int16])(nil)
	_ Codec[int32]   = (*RunendCodec[int32])(nil)
	_ Codec[int64]   = (*RunendCodec[int64])(nil)
	_ Codec[uint8]   = (*RunendCodec[uint8])(nil)
	_ Codec[uint16]  = (*RunendCodec[uint16])(nil)
	_ Codec[uint32]  = (*RunendCodec[uint32])(nil)
	_ Codec[uint64]  = (*RunendCodec[uint64])(nil)
	_ Codec[float32] = (*RunendCodec[float32])(nil)
	_ Codec[float64] = (*RunendCodec[float64])(nil)
	_ Codec[string]  = (*RunendCodec[string])(nil)
)

type RunendCodec[T Integer | Float | String] struct {
	length uint64
	runs   Codec[T]
	ends   Codec[uint64]
}

func newRunendCodec[T Integer | Float | String](data []T, cmpFn cmpFn[T], depth int) (*RunendCodec[T], error) {
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
	runsCodec := compress(runs, depth-1)
	endsCodec := CompressInteger(array.NewPrimitivesUnsafe(ends), depth-1)
	return &RunendCodec[T]{length: uint64(len(data)), runs: runsCodec, ends: endsCodec}, nil
}

func NewRunendIntegerCodec[T Integer](data []T, depth int) (*RunendCodec[T], error) {
	return newRunendCodec(data, cmpIntegers[T], depth)
}

func NewRunendStringCodec[T String](data []T, depth int) (*RunendCodec[T], error) {
	return newRunendCodec(data, cmpStrings[T], depth)
}

func NewRunendFloatCodec[T Float](data []T, depth int) (*RunendCodec[T], error) {
	return newRunendCodec(data, cmpFloats[T], depth)
}

func (r *RunendCodec[T]) Children() []Scheme { return []Scheme{r.runs.(Scheme), r.ends.(Scheme)} }

func (r *RunendCodec[T]) ValueAt(offset uint64) (T, error) {
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
		if offset < end {
			hi = mid
			continue
		}
		lo = mid + 1
	}

	return r.runs.ValueAt(lo)
}

func (r *RunendCodec[T]) WriteTo(w io.Writer) (n int64, err error) {
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

func (r *RunendCodec[T]) BinarySize() uint64 {
	return uint64(headerSize) + r.runs.BinarySize() + r.ends.BinarySize()
}
func (r *RunendCodec[T]) Length() uint64 { return r.length }
func (r *RunendCodec[T]) PType() PType   { return pTypeForType[T]() }
