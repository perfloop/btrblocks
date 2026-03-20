package btrblocks

import (
	"fmt"
	"io"

	"github.com/axiomhq/btrblocks/array"
)

type runEndCodec[T Integer | Float | String, U UnsignedInteger] struct {
	length uint64
	runs   Codec[T]
	ends   Codec[U]
}

func validateRunEndChildren[T Integer | Float | String, U UnsignedInteger](length uint64, runs Codec[T], ends Codec[U]) error {
	if runs.Length() != ends.Length()+1 {
		return fmt.Errorf("codec: runend runs length = %d, want %d", runs.Length(), ends.Length()+1)
	}
	if ends.Length() == 0 {
		return nil
	}
	first := uint64(ends.ValueAt(0))
	if first == 0 {
		return fmt.Errorf("codec: runend first end = 0, want > 0")
	}
	last := uint64(ends.ValueAt(ends.Length() - 1))
	if last >= length {
		return fmt.Errorf("codec: runend last end = %d, want < %d", last, length)
	}
	return nil
}

func (r *runEndCodec[T, U]) Kind() CodeType { return CodecTypeRunEnd }
func (r *runEndCodec[T, U]) Length() uint64 { return r.length }
func (r *runEndCodec[T, U]) PType() PType   { return pTypeForType[T]() }

func (r *runEndCodec[T, U]) BinarySize() uint64 {
	return uint64(headerSize) + r.runs.BinarySize() + r.ends.BinarySize()
}

func (r *runEndCodec[T, U]) ValueAt(offset uint64) T {
	if offset >= r.length {
		panic(errOffsetOutOfRange)
	}
	lo, hi := uint64(0), r.ends.Length()
	for lo < hi {
		mid := lo + (hi-lo)/2
		if offset < uint64(r.ends.ValueAt(mid)) {
			hi = mid
			continue
		}
		lo = mid + 1
	}
	return r.runs.ValueAt(lo)
}

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

func (r *runEndCodec[T, U]) Decode(dst []T) error {
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

func (r *runEndCodec[T, U]) WriteTo(w io.Writer) (int64, error) {
	n, err := header{
		Version:  versionNumber,
		Kind:     CodecTypeRunEnd,
		ElemType: pTypeForType[T](),
		Length:   r.length,
		BodySize: 0,
	}.WriteTo(w)
	if err != nil {
		return n, err
	}
	nn, err := r.runs.WriteTo(w)
	n += nn
	if err != nil {
		return n, err
	}
	nn, err = r.ends.WriteTo(w)
	return n + nn, err
}

func readRunEndCodec[T Integer | Float | String](r io.Reader, h header) (Codec[T], error) {
	if h.BodySize != 0 {
		return nil, fmt.Errorf("codec: runend body size = %d, want 0", h.BodySize)
	}
	if h.Length == 0 {
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
		ends, err := readCodecWithHeader[uint8](r, childHeader)
		if err != nil {
			return nil, err
		}
		if err := validateRunEndChildren(h.Length, runs, ends); err != nil {
			return nil, err
		}
		return &runEndCodec[T, uint8]{length: h.Length, runs: runs, ends: ends}, nil
	case PTypeUint16:
		ends, err := readCodecWithHeader[uint16](r, childHeader)
		if err != nil {
			return nil, err
		}
		if err := validateRunEndChildren(h.Length, runs, ends); err != nil {
			return nil, err
		}
		return &runEndCodec[T, uint16]{length: h.Length, runs: runs, ends: ends}, nil
	case PTypeUint32:
		ends, err := readCodecWithHeader[uint32](r, childHeader)
		if err != nil {
			return nil, err
		}
		if err := validateRunEndChildren(h.Length, runs, ends); err != nil {
			return nil, err
		}
		return &runEndCodec[T, uint32]{length: h.Length, runs: runs, ends: ends}, nil
	case PTypeUint64:
		ends, err := readCodecWithHeader[uint64](r, childHeader)
		if err != nil {
			return nil, err
		}
		if err := validateRunEndChildren(h.Length, runs, ends); err != nil {
			return nil, err
		}
		return &runEndCodec[T, uint64]{length: h.Length, runs: runs, ends: ends}, nil
	default:
		return nil, fmt.Errorf("codec: runend end type = %v, want unsigned integer", childHeader.ElemType)
	}
}

func buildRunEndCodec[T Integer | Float | String](arr array.Array[T], ctx planContext, cmp cmpFn[T]) (Codec[T], error) {
	if ctx.depth <= 0 {
		return nil, errDepthExhausted
	}
	if arr.Length() == 0 {
		return nil, errDataEmpty
	}

	runs := make([]T, 0)
	ends := make([]uint64, 0)
	prev := arr.ValueAt(0)
	runs = append(runs, prev)
	for i := uint64(1); i < arr.Length(); i++ {
		value := arr.ValueAt(i)
		if !cmp(prev, value) {
			ends = append(ends, i)
			runs = append(runs, value)
			prev = value
		}
	}

	runsChildCtx := withTypeExcludes[T](ctx.descend(), CodecTypeRunEnd, CodecTypeDict)
	runsCodec, err := compressArray(buildArray(runs), runsChildCtx)
	if err != nil {
		return nil, err
	}
	endsChildCtx := ctx.descend().withIntegerExcludes(CodecTypeRunEnd, CodecTypeDict)
	maxEnd := uint64(0)
	if len(ends) > 0 {
		maxEnd = ends[len(ends)-1]
	}
	switch {
	case maxEnd <= uint64(^uint8(0)):
		narrow := make([]uint8, len(ends))
		for i, end := range ends {
			narrow[i] = uint8(end)
		}
		endsCodec, err := compressArray[uint8](array.NewPrimitivesUnsafe(narrow), endsChildCtx)
		if err != nil {
			return nil, err
		}
		return &runEndCodec[T, uint8]{length: arr.Length(), runs: runsCodec, ends: endsCodec}, nil
	case maxEnd <= uint64(^uint16(0)):
		narrow := make([]uint16, len(ends))
		for i, end := range ends {
			narrow[i] = uint16(end)
		}
		endsCodec, err := compressArray[uint16](array.NewPrimitivesUnsafe(narrow), endsChildCtx)
		if err != nil {
			return nil, err
		}
		return &runEndCodec[T, uint16]{length: arr.Length(), runs: runsCodec, ends: endsCodec}, nil
	case maxEnd <= uint64(^uint32(0)):
		narrow := make([]uint32, len(ends))
		for i, end := range ends {
			narrow[i] = uint32(end)
		}
		endsCodec, err := compressArray[uint32](array.NewPrimitivesUnsafe(narrow), endsChildCtx)
		if err != nil {
			return nil, err
		}
		return &runEndCodec[T, uint32]{length: arr.Length(), runs: runsCodec, ends: endsCodec}, nil
	default:
		narrow := make([]uint64, len(ends))
		copy(narrow, ends)
		endsCodec, err := compressArray[uint64](array.NewPrimitivesUnsafe(narrow), endsChildCtx)
		if err != nil {
			return nil, err
		}
		return &runEndCodec[T, uint64]{length: arr.Length(), runs: runsCodec, ends: endsCodec}, nil
	}
}

func estimateRunEnd[T Integer | Float | String, S statsSource[T]](avgRunLength float64, cmp cmpFn[T]) func(S, planContext) (float64, bool) {
	return func(stats S, ctx planContext) (float64, bool) {
		if ctx.depth <= 0 || avgRunLength < 4 {
			return 0, false
		}
		return estimateBySample(stats, ctx, func(arr array.Array[T], ctx planContext) (Codec[T], error) {
			return buildRunEndCodec(arr, ctx, cmp)
		})
	}
}
