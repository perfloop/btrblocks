package btrblocks

import (
	"fmt"
	"io"

	"github.com/axiomhq/btrblocks/array"
)

// runEndArray stores run values plus ordinal run-end boundaries.
type runEndArray[T Integer | Float | String] struct {
	length uint64
	runs   EncodedArray[T]
	ends   ordinalArray
}

func validateRunEndChildren[T Integer | Float | String](length uint64, runs EncodedArray[T], ends ordinalArray) error {
	if runs.Length() != ends.Length()+1 {
		return fmt.Errorf("codec: runend runs length = %d, want %d", runs.Length(), ends.Length()+1)
	}
	if ends.Length() == 0 {
		return nil
	}
	first := ends.ValueAt(0)
	if first == 0 {
		return fmt.Errorf("codec: runend first end = 0, want > 0")
	}
	last := ends.ValueAt(ends.Length() - 1)
	if last >= length {
		return fmt.Errorf("codec: runend last end = %d, want < %d", last, length)
	}
	return nil
}

func (r *runEndArray[T]) Encoding() CodeType { return CodecTypeRunEnd }
func (r *runEndArray[T]) Length() uint64     { return r.length }
func (r *runEndArray[T]) PType() PType       { return pTypeForType[T]() }

func (r *runEndArray[T]) BinarySize() uint64 {
	return uint64(headerSize) + r.runs.BinarySize() + r.ends.BinarySize()
}

func (r *runEndArray[T]) ValueAt(offset uint64) T {
	if offset >= r.length {
		panic(errOffsetOutOfRange)
	}
	lo, hi := uint64(0), r.ends.Length()
	for lo < hi {
		mid := lo + (hi-lo)/2
		if offset < r.ends.ValueAt(mid) {
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

func (r *runEndArray[T]) CopyTo(dst []T) error {
	if err := validateCopyLength(r.length, len(dst)); err != nil {
		return err
	}
	runs := make([]T, r.runs.Length())
	if err := r.runs.CopyTo(runs); err != nil {
		return err
	}
	ends := make([]uint64, r.ends.Length())
	if err := r.ends.CopyToU64(ends); err != nil {
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

func (r *runEndArray[T]) Slice(start, end uint64) (EncodedArray[T], error) {
	return sliceToRawArray[T](r, start, end)
}

func (r *runEndArray[T]) WriteTo(w io.Writer) (int64, error) {
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

func readRunEndArray[T Integer | Float | String](r io.Reader, h header) (EncodedArray[T], error) {
	if h.BodySize != 0 {
		return nil, fmt.Errorf("codec: runend body size = %d, want 0", h.BodySize)
	}
	if h.Length == 0 {
		return nil, fmt.Errorf("codec: runend length = 0")
	}
	runs, err := readEncodedArray[T](r)
	if err != nil {
		return nil, err
	}
	childHeader, err := readHeader(r)
	if err != nil {
		return nil, err
	}
	ends, err := readOrdinalArray(r, childHeader)
	if err != nil {
		return nil, fmt.Errorf("codec: runend end %w", err)
	}
	if err := validateRunEndChildren(h.Length, runs, ends); err != nil {
		return nil, err
	}
	return &runEndArray[T]{length: h.Length, runs: runs, ends: ends}, nil
}

func buildRunEndArray[T Integer | Float | String](arr array.Array[T], ctx planContext, cmp cmpFn[T]) (EncodedArray[T], error) {
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
	endsCodec, err := buildCompressedOrdinals(ends, ctx.descend(), CodecTypeRunEnd, CodecTypeDict)
	if err != nil {
		return nil, err
	}
	return &runEndArray[T]{length: arr.Length(), runs: runsCodec, ends: endsCodec}, nil
}

func estimateRunEnd[T Integer | Float | String, S statsSource[T]](avgRunLength float64, cmp cmpFn[T]) func(S, planContext) (float64, bool) {
	return func(stats S, ctx planContext) (float64, bool) {
		if ctx.depth <= 0 || avgRunLength < 4 {
			return 0, false
		}
		return estimateBySample(stats, ctx, func(arr array.Array[T], ctx planContext) (EncodedArray[T], error) {
			return buildRunEndArray(arr, ctx, cmp)
		})
	}
}
