package codec

import (
	"fmt"
	"io"

	"github.com/axiomhq/btrblocks/array"
)

// runEndArray stores run values plus ordinal run-end boundaries.
type runEndArray[V Integer | Float | String, I UnsignedInteger] struct {
	denseRows
	runs  EncodedArray[V]
	ends  EncodedArray[I]
	slice sliceBuilder[V]
}

func validateRunEndChildren[V Integer | Float | String, I UnsignedInteger](length uint64, runs EncodedArray[V], ends EncodedArray[I]) error {
	if runs.Length() != ends.Length()+1 {
		return fmt.Errorf("codec: runend runs length = %d, want %d", runs.Length(), ends.Length()+1)
	}
	n := ends.Length()
	if n == 0 {
		return nil
	}
	// Validate ALL ends are strictly increasing and in range (0, length).
	prev := uint64(0)
	for i := range n {
		end := uint64(ends.ValueAt(i))
		if end == 0 {
			return fmt.Errorf("codec: runend end = 0 at position %d, want > 0", i)
		}
		if end >= length {
			return fmt.Errorf("codec: runend end = %d at position %d, want < %d", end, i, length)
		}
		if i > 0 && end <= prev {
			return fmt.Errorf("codec: runend ends not strictly increasing at position %d: %d <= %d", i, end, prev)
		}
		prev = end
	}
	return nil
}

func (r *runEndArray[V, I]) CodecType() CodecType {
	return CodecTypeRunEnd
}
func (r *runEndArray[V, I]) PType() PType { return r.runs.PType() }
func (r *runEndArray[V, I]) DecodedBytes() (uint64, error) {
	return decodedBytesFor(r.Length(), r.PType())
}

func (r *runEndArray[V, I]) BinarySize() uint64 {
	return uint64(headerSize) + r.runs.BinarySize() + r.ends.BinarySize()
}

func (r *runEndArray[V, I]) ValueAt(offset uint64) V {
	if offset >= r.Length() {
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
	if err := checkDstLen(dst, r.Length()); err != nil {
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
	fillRun(dst, pos, int(r.Length()), runs[len(ends)])
	return nil
}

func (r *runEndArray[V, I]) Slice(start, end uint64) (EncodedArray[V], error) {
	return r.slice(r, start, end)
}

func (r *runEndArray[V, I]) WriteTo(w io.Writer) (int64, error) {
	sum := writeSum{w: w}
	if err := sum.writeTo(codecHeader{
		Version:  versionNumber,
		Type:     CodecTypeRunEnd,
		ElemType: r.runs.PType(),
		Length:   r.Length(),
		NumBytes: 0,
	}); err != nil {
		return sum.n, err
	}
	if err := sum.writeTo(r.runs); err != nil {
		return sum.n, err
	}
	if err := sum.writeTo(r.ends); err != nil {
		return sum.n, err
	}
	return sum.n, nil
}

func readRunEndArray[V Integer | Float | String](br *array.BufReader, h codecHeader, opts ReadOptions, readValues encodedReader[V], slice sliceBuilder[V]) (EncodedArray[V], error) {
	if h.NumBytes != 0 {
		return nil, fmt.Errorf("codec: runend body size = %d, want 0", h.NumBytes)
	}
	if h.Length == 0 {
		return nil, fmt.Errorf("codec: runend length = 0")
	}
	runs, err := readValues(br, opts)
	if err != nil {
		return nil, fmt.Errorf("codec: reading runend runs: %w", err)
	}
	if err := requireNonNullable(runs, "run-end values"); err != nil {
		return nil, err
	}
	childHeader, err := readHeader(br)
	if err != nil {
		return nil, fmt.Errorf("codec: reading runend ends header: %w", err)
	}
	switch childHeader.ElemType {
	case PTypeUint8:
		return readRunEndOrdinals[V, uint8](br, h, childHeader, opts, runs, slice)
	case PTypeUint16:
		return readRunEndOrdinals[V, uint16](br, h, childHeader, opts, runs, slice)
	case PTypeUint32:
		return readRunEndOrdinals[V, uint32](br, h, childHeader, opts, runs, slice)
	case PTypeUint64:
		return readRunEndOrdinals[V, uint64](br, h, childHeader, opts, runs, slice)
	default:
		return nil, fmt.Errorf("codec: runend ordinal child type = %v, want unsigned integer", childHeader.ElemType)
	}
}

func readRunEndOrdinals[V Integer | Float | String, I UnsignedInteger](br *array.BufReader, h codecHeader, childHeader codecHeader, opts ReadOptions, runs EncodedArray[V], slice sliceBuilder[V]) (EncodedArray[V], error) {
	ends, err := readUnsignedEncodedArrayWithHeader[I](br, childHeader, opts)
	if err != nil {
		return nil, fmt.Errorf("codec: reading runend ends: %w", err)
	}
	if err := requireNonNullable(ends, "run-end ends"); err != nil {
		return nil, err
	}
	if err := validateRunEndChildren(h.Length, runs, ends); err != nil {
		return nil, err
	}
	return &runEndArray[V, I]{denseRows: denseRows(h.Length), runs: runs, ends: ends, slice: slice}, nil
}

// encodePrimitiveRunEndAs extracts numeric runs and end positions and delegates
// both typed children to the caller.
func encodePrimitiveRunEndAs[V Integer | Float, I UnsignedInteger](arr array.ArrayCore[V], cmp func(V, V) bool, buildRuns childBuilder[V], buildEnds childBuilder[I], budget buildBudget) (EncodedArray[V], error) {
	return buildRunEndAs(arr, cmp, buildRuns, buildEnds, slicePrimitiveToRawArray[V], budget)
}

// encodeStringRunEndAs extracts string runs and end positions and delegates
// both typed children to the caller.
func encodeStringRunEndAs[I UnsignedInteger](arr array.ArrayCore[string], cmp func(string, string) bool, buildRuns childBuilder[string], buildEnds childBuilder[I], budget buildBudget) (EncodedArray[string], error) {
	return buildRunEndAs(arr, cmp, buildRuns, buildEnds, sliceStringToRawArray, budget)
}

func buildRunEndAs[V Integer | Float | String, I UnsignedInteger](arr array.ArrayCore[V], cmp func(V, V) bool, buildRuns childBuilder[V], buildEnds childBuilder[I], slice sliceBuilder[V], budget buildBudget) (EncodedArray[V], error) {
	if buildRuns == nil || buildEnds == nil {
		return nil, ErrBuilderRequired
	}
	if arr.Length() == 0 {
		return nil, ErrDataEmpty
	}
	// Pre-size for estimated run count. With avg run length 10 (the minimum
	// threshold for RLE viability is 4), n/8 is a reasonable upper-bound
	// estimate that avoids most append reallocations.
	n := arr.Length()
	if n > uint64(^uint(0)>>1) {
		return nil, fmt.Errorf("codec: run-end length %d overflows int", n)
	}

	// Avoid trusting a declared length as an allocation request. The buffers
	// grow with runs actually observed while retaining a useful small hint.
	estRuns := min(max(n/8, 8), 4096)
	runs, err := makeBuildSlice[V](budget, 0, cappedBuildCapacity[V](budget, estRuns), "run values")
	if err != nil {
		return nil, err
	}
	ends, err := makeBuildSlice[I](budget, 0, cappedBuildCapacity[I](budget, estRuns), "run ends")
	if err != nil {
		return nil, err
	}
	prev := arr.ValueAt(0)
	runs, err = appendBuildValue(runs, prev, budget, "run values")
	if err != nil {
		return nil, err
	}
	for i := uint64(1); i < n; i++ {
		value := arr.ValueAt(i)
		if !cmp(prev, value) {
			ends, err = appendBuildValue(ends, I(i), budget, "run ends")
			if err != nil {
				return nil, err
			}
			runs, err = appendBuildValue(runs, value, budget, "run values")
			if err != nil {
				return nil, err
			}
			prev = value
		}
	}

	runsCodec, err := buildRuns(sliceArrayCore[V](runs))
	if err != nil {
		return nil, fmt.Errorf("codec: compress run values: %w", err)
	}

	endsCodec, err := buildEnds(array.NewPrimitivesUnsafe(ends))
	if err != nil {
		return nil, fmt.Errorf("codec: compress run ends: %w", err)
	}
	return &runEndArray[V, I]{denseRows: denseRows(arr.Length()), runs: runsCodec, ends: endsCodec, slice: slice}, nil
}
