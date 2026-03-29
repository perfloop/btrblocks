package btrblocks

import (
	"fmt"
	"io"

	"github.com/axiomhq/btrblocks/array"
)

// runEndArray stores run values plus ordinal run-end boundaries.
type runEndArray[V Integer | Float | String, I UnsignedInteger] struct {
	length uint64
	runs   EncodedArray[V]
	ends   EncodedArray[I]
}

func validateRunEndChildren[V Integer | Float | String, I UnsignedInteger](length uint64, runs EncodedArray[V], ends EncodedArray[I]) error {
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

func (r *runEndArray[V, I]) Encoding() CodeType { return CodecTypeRunEnd }
func (r *runEndArray[V, I]) Length() uint64     { return r.length }
func (r *runEndArray[V, I]) PType() PType       { return array.PTypeForType[V]() }

func (r *runEndArray[V, I]) BinarySize() uint64 {
	return uint64(headerSize) + r.runs.BinarySize() + r.ends.BinarySize()
}

func (r *runEndArray[V, I]) ValueAt(offset uint64) V {
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

// DecompressInto decodes the run-end array into dst. It bulk-decodes both
// children into intermediate slices (sized by run count, not N), then fills dst
// with memcpy-doubling. Per-element ValueAt was benchmarked and is ~10% slower
// at 10K elements despite the intermediates being small — the overhead comes
// from per-call interface dispatch and bitpack unpacking through the codec tree.
func (r *runEndArray[V, I]) DecompressInto(dst []V) error {
	if err := checkDstLen(dst, r.length); err != nil {
		return err
	}
	runs, err := Decompress(r.runs)
	if err != nil {
		return err
	}
	ends, err := Decompress(r.ends)
	if err != nil {
		return err
	}
	pos := 0
	for i, rawEnd := range ends {
		fillRun(dst, pos, int(uint64(rawEnd)), runs[i])
		pos = int(uint64(rawEnd))
	}
	fillRun(dst, pos, int(r.length), runs[len(ends)])
	return nil
}

func (r *runEndArray[V, I]) Slice(start, end uint64) (EncodedArray[V], error) {
	return sliceToRawArray(r, start, end)
}

func (r *runEndArray[V, I]) WriteTo(w io.Writer) (int64, error) {
	n, err := codecHeader{
		Version:  versionNumber,
		Kind:     CodecTypeRunEnd,
		ElemType: array.PTypeForType[V](),
		Length:   r.length,
		NumBytes: 0,
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

func readRunEndArray[V Integer | Float | String](br *array.BufReader, h codecHeader, opts ReadOptions) (EncodedArray[V], error) {
	if h.NumBytes != 0 {
		return nil, fmt.Errorf("codec: runend body size = %d, want 0", h.NumBytes)
	}
	if h.Length == 0 {
		return nil, fmt.Errorf("codec: runend length = 0")
	}
	runs, err := readEncodedArray[V](br, opts)
	if err != nil {
		return nil, err
	}
	childHeader, err := readHeader(br)
	if err != nil {
		return nil, err
	}
	switch childHeader.ElemType {
	case PTypeUint8:
		return readRunEndOrdinals[V, uint8](br, h, childHeader, opts, runs)
	case PTypeUint16:
		return readRunEndOrdinals[V, uint16](br, h, childHeader, opts, runs)
	case PTypeUint32:
		return readRunEndOrdinals[V, uint32](br, h, childHeader, opts, runs)
	case PTypeUint64:
		return readRunEndOrdinals[V, uint64](br, h, childHeader, opts, runs)
	default:
		return nil, fmt.Errorf("codec: runend ordinal child type = %v, want unsigned integer", childHeader.ElemType)
	}
}

func readRunEndOrdinals[V Integer | Float | String, I UnsignedInteger](br *array.BufReader, h codecHeader, childHeader codecHeader, opts ReadOptions, runs EncodedArray[V]) (EncodedArray[V], error) {
	ends, err := readEncodedArrayWithHeader[I](br, childHeader, opts)
	if err != nil {
		return nil, fmt.Errorf("codec: runend end %w", err)
	}
	if err := validateRunEndChildren(h.Length, runs, ends); err != nil {
		return nil, err
	}
	return &runEndArray[V, I]{length: h.Length, runs: runs, ends: ends}, nil
}

func buildRunEndArray[V Integer | Float | String](arr array.ArrayCore[V], ctx planContext, cmp cmpFn[V]) (EncodedArray[V], error) {
	if ctx.depth <= 0 {
		return nil, errDepthExhausted
	}
	n := arr.Length()
	if n == 0 {
		return nil, errDataEmpty
	}

	// Pick I from arr.Length() — ends are positions in [0, N), so max end < N.
	switch {
	case n <= 1<<8:
		return buildRunEndWithEnds[V, uint8](arr, ctx, cmp)
	case n <= 1<<16:
		return buildRunEndWithEnds[V, uint16](arr, ctx, cmp)
	case n <= 1<<32:
		return buildRunEndWithEnds[V, uint32](arr, ctx, cmp)
	default:
		return buildRunEndWithEnds[V, uint64](arr, ctx, cmp)
	}
}

// buildRunEndWithEnds scans the array once, building runs and []I ends directly.
func buildRunEndWithEnds[V Integer | Float | String, I UnsignedInteger](arr array.ArrayCore[V], ctx planContext, cmp cmpFn[V]) (EncodedArray[V], error) {
	// Pre-size for estimated run count. With avg run length 10 (the minimum
	// threshold for RLE viability is 4), n/8 is a reasonable upper-bound
	// estimate that avoids most append reallocations.
	n := arr.Length()
	estRuns := int(n / 8)
	if estRuns < 8 {
		estRuns = 8
	}
	runs := make([]V, 0, estRuns)
	ends := make([]I, 0, estRuns)
	prev := arr.ValueAt(0)
	runs = append(runs, prev)
	for i := uint64(1); i < n; i++ {
		value := arr.ValueAt(i)
		if !cmp(prev, value) {
			ends = append(ends, I(i))
			runs = append(runs, value)
			prev = value
		}
	}

	runsChildCtx := withTypeExcludes[V](ctx.descend(), CodecTypeRunEnd, CodecTypeDict)
	runsCodec, err := compressArray(buildArray(runs), runsChildCtx)
	if err != nil {
		return nil, err
	}

	endsChildCtx := ctx.descend().withIntegerExcludes(CodecTypeRunEnd, CodecTypeDict)
	endsCodec, err := compressArray(array.NewPrimitivesUnsafe(ends), endsChildCtx)
	if err != nil {
		return nil, err
	}
	return &runEndArray[V, I]{length: arr.Length(), runs: runsCodec, ends: endsCodec}, nil
}

func estimateRunEnd[T Integer | Float | String, S statsSource[T]](stats S, ctx planContext, avgRunLength float64, cmp cmpFn[T]) (float64, bool) {
	if ctx.depth <= 0 || avgRunLength < 4 {
		return 0, false
	}
	return estimateBySample(stats, ctx, func(arr array.ArrayCore[T], ctx planContext) (EncodedArray[T], error) {
		return buildRunEndArray(arr, ctx, cmp)
	})
}
