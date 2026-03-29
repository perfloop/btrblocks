package btrblocks

import (
	"fmt"
	"io"

	"github.com/axiomhq/btrblocks/array"
)

// dictArray stores unique values plus an ordinal child that indexes into them.
type dictArray[V Integer | Float | String, I UnsignedInteger] struct {
	values  EncodedArray[V]
	indices EncodedArray[I]
}

func (d *dictArray[V, I]) Encoding() CodeType { return CodecTypeDict }
func (d *dictArray[V, I]) Length() uint64     { return d.indices.Length() }
func (d *dictArray[V, I]) PType() PType       { return array.PTypeForType[V]() }

func (d *dictArray[V, I]) BinarySize() uint64 {
	return uint64(headerSize) + d.values.BinarySize() + d.indices.BinarySize()
}

func (d *dictArray[V, I]) ValueAt(offset uint64) V {
	return d.values.ValueAt(uint64(d.indices.ValueAt(offset)))
}

// DecompressInto decodes the dictionary into dst. It bulk-decodes both the
// values and indices children into intermediate slices, then performs a single
// gather pass. Per-element ValueAt was benchmarked and is 3.3x slower at 10K
// elements because it loses the batch-optimized unpackBatchTyped fast path in
// the bitpacked indices child — each call traverses the codec tree individually
// instead of one bulk decode. The intermediate allocation (~N * index_width
// bytes) is the cost of keeping the fast path.
func (d *dictArray[V, I]) DecompressInto(dst []V) error {
	if err := checkDstLen(dst, d.indices.Length()); err != nil {
		return err
	}
	values, err := Decompress(d.values)
	if err != nil {
		return err
	}
	indices, err := Decompress(d.indices)
	if err != nil {
		return err
	}
	for i, idx := range indices {
		dst[i] = values[idx]
	}
	return nil
}

func (d *dictArray[V, I]) Slice(start, end uint64) (EncodedArray[V], error) {
	indices, err := d.indices.Slice(start, end)
	if err != nil {
		return nil, err
	}
	return &dictArray[V, I]{values: d.values, indices: indices}, nil
}

func (d *dictArray[V, I]) WriteTo(w io.Writer) (int64, error) {
	n, err := codecHeader{
		Version:  versionNumber,
		Kind:     CodecTypeDict,
		ElemType: array.PTypeForType[V](),
		Length:   d.indices.Length(),
		NumBytes: 0,
	}.WriteTo(w)
	if err != nil {
		return n, err
	}
	nn, err := d.values.WriteTo(w)
	n += nn
	if err != nil {
		return n, err
	}
	nn, err = d.indices.WriteTo(w)
	return n + nn, err
}

func readDictArray[V Integer | Float | String](br *array.BufReader, h codecHeader, opts ReadOptions) (EncodedArray[V], error) {
	if h.NumBytes != 0 {
		return nil, fmt.Errorf("codec: dict body size = %d, want 0", h.NumBytes)
	}
	values, err := readEncodedArray[V](br, opts)
	if err != nil {
		return nil, err
	}
	childHeader, err := readHeader(br)
	if err != nil {
		return nil, err
	}
	switch childHeader.ElemType {
	case PTypeUint8:
		return readDictOrdinals[V, uint8](br, h, childHeader, opts, values)
	case PTypeUint16:
		return readDictOrdinals[V, uint16](br, h, childHeader, opts, values)
	case PTypeUint32:
		return readDictOrdinals[V, uint32](br, h, childHeader, opts, values)
	case PTypeUint64:
		return readDictOrdinals[V, uint64](br, h, childHeader, opts, values)
	default:
		return nil, fmt.Errorf("codec: dict ordinal child type = %v, want unsigned integer", childHeader.ElemType)
	}
}

func readDictOrdinals[V Integer | Float | String, I UnsignedInteger](br *array.BufReader, h codecHeader, childHeader codecHeader, opts ReadOptions, values EncodedArray[V]) (EncodedArray[V], error) {
	indices, err := readEncodedArrayWithHeader[I](br, childHeader, opts)
	if err != nil {
		return nil, fmt.Errorf("codec: dict index %w", err)
	}
	if indices.Length() != h.Length {
		return nil, fmt.Errorf("codec: dict length = %d, want %d", indices.Length(), h.Length)
	}
	return &dictArray[V, I]{values: values, indices: indices}, nil
}

// buildIntegerDictFromDistinct builds a dictionary-encoded array using
// pre-computed distinct values from stats when available. Falls back to
// scanning the array if distinct is empty (e.g. during sample estimation).
func buildIntegerDictFromDistinct[T Integer](arr array.ArrayCore[T], distinct map[T]uint64, ctx planContext) (EncodedArray[T], error) {
	if ctx.depth <= 0 {
		return nil, errDepthExhausted
	}

	n := arr.Length()
	if len(distinct) == 0 {
		distinct = make(map[T]uint64)
		for i := range n {
			v := arr.ValueAt(i)
			if _, ok := distinct[v]; !ok {
				distinct[v] = uint64(len(distinct))
			}
		}
	}

	values := make([]T, len(distinct))
	for v, ord := range distinct {
		values[ord] = v
	}

	valuesCodec, err := compressArray(buildArray(values), ctx.descend().withIntegerExcludes(CodecTypeDict))
	if err != nil {
		return nil, err
	}

	// Pick index width from dict size — max code is len(distinct)-1.
	// Build []I directly, never allocating []uint64.
	numDistinct := len(distinct)
	switch {
	case numDistinct <= 1<<8:
		return buildDictWithCodes[T, uint8](arr, distinct, valuesCodec, ctx)
	case numDistinct <= 1<<16:
		return buildDictWithCodes[T, uint16](arr, distinct, valuesCodec, ctx)
	default:
		return buildDictWithCodes[T, uint32](arr, distinct, valuesCodec, ctx)
	}
}

// buildDictWithCodes builds the codes array as []I directly from the distinct map.
func buildDictWithCodes[T Integer, I UnsignedInteger](arr array.ArrayCore[T], distinct map[T]uint64, valuesCodec EncodedArray[T], ctx planContext) (EncodedArray[T], error) {
	n := arr.Length()
	codes := make([]I, n)
	for i := range n {
		codes[i] = I(distinct[arr.ValueAt(i)])
	}
	childCtx := ctx.descend().withIntegerExcludes(CodecTypeDict, CodecTypeSequence)
	codesCodec, err := compressArray(array.NewPrimitivesUnsafe(codes), childCtx)
	if err != nil {
		return nil, err
	}
	return &dictArray[T, I]{values: valuesCodec, indices: codesCodec}, nil
}

// buildFloatDictFromDistinct builds a dictionary-encoded array using
// pre-computed distinct values from stats, avoiding a redundant array scan.
// If distinct is empty (e.g. during sample estimation), it falls back to
// scanning the array.
func buildFloatDictFromDistinct[T Float](arr array.ArrayCore[T], distinct map[uint64]uint64, ctx planContext) (EncodedArray[T], error) {
	if ctx.depth <= 0 {
		return nil, errDepthExhausted
	}

	n := arr.Length()
	if len(distinct) == 0 {
		distinct = make(map[uint64]uint64)
		for i := range n {
			key := floatBits(arr.ValueAt(i))
			if _, ok := distinct[key]; !ok {
				distinct[key] = uint64(len(distinct))
			}
		}
	}

	values := make([]T, len(distinct))
	for bits, ord := range distinct {
		values[ord] = floatFromBits[T](bits)
	}

	valuesCodec, err := compressArray(buildArray(values), ctx.descend().withFloatExcludes(CodecTypeDict))
	if err != nil {
		return nil, err
	}

	numDistinct := len(distinct)
	switch {
	case numDistinct <= 1<<8:
		return buildFloatDictWithCodes[T, uint8](arr, distinct, valuesCodec, ctx)
	case numDistinct <= 1<<16:
		return buildFloatDictWithCodes[T, uint16](arr, distinct, valuesCodec, ctx)
	default:
		return buildFloatDictWithCodes[T, uint32](arr, distinct, valuesCodec, ctx)
	}
}

func buildFloatDictWithCodes[T Float, I UnsignedInteger](arr array.ArrayCore[T], distinct map[uint64]uint64, valuesCodec EncodedArray[T], ctx planContext) (EncodedArray[T], error) {
	n := arr.Length()
	codes := make([]I, n)
	for i := range n {
		codes[i] = I(distinct[floatBits(arr.ValueAt(i))])
	}
	childCtx := ctx.descend().withIntegerExcludes(CodecTypeDict, CodecTypeSequence)
	codesCodec, err := compressArray(array.NewPrimitivesUnsafe(codes), childCtx)
	if err != nil {
		return nil, err
	}
	return &dictArray[T, I]{values: valuesCodec, indices: codesCodec}, nil
}

func buildStringDictArray[T String](arr array.ArrayCore[T], ctx planContext) (EncodedArray[T], error) {
	if ctx.depth <= 0 {
		return nil, errDepthExhausted
	}
	n := arr.Length()
	dict := make(map[T]uint64)
	values := make([]T, 0, n)
	for i := range n {
		value := arr.ValueAt(i)
		if _, ok := dict[value]; !ok {
			dict[value] = uint64(len(values))
			values = append(values, value)
		}
	}

	valuesCodec, err := compressArray(buildArray(values), ctx.descend().withStringExcludes(CodecTypeDict))
	if err != nil {
		return nil, err
	}

	numDistinct := len(dict)
	switch {
	case numDistinct <= 1<<8:
		return buildStringDictWithCodes[T, uint8](arr, dict, valuesCodec, ctx)
	case numDistinct <= 1<<16:
		return buildStringDictWithCodes[T, uint16](arr, dict, valuesCodec, ctx)
	default:
		return buildStringDictWithCodes[T, uint32](arr, dict, valuesCodec, ctx)
	}
}

func buildStringDictWithCodes[T String, I UnsignedInteger](arr array.ArrayCore[T], dict map[T]uint64, valuesCodec EncodedArray[T], ctx planContext) (EncodedArray[T], error) {
	n := arr.Length()
	codes := make([]I, n)
	for i := range n {
		codes[i] = I(dict[arr.ValueAt(i)])
	}
	childCtx := ctx.descend().withIntegerExcludes(CodecTypeDict, CodecTypeSequence)
	codesCodec, err := compressArray(array.NewPrimitivesUnsafe(codes), childCtx)
	if err != nil {
		return nil, err
	}
	return &dictArray[T, I]{values: valuesCodec, indices: codesCodec}, nil
}

func estimateIntegerDict[T Integer, S statsSource[T]](stats S, ctx planContext, distinctCount uint64, avgRunLength float64) (float64, bool) {
	if ctx.depth <= 0 {
		return 0, false
	}

	n := stats.Source().Length()
	if n == 0 || distinctCount <= 1 || distinctCount > n/2 {
		return 0, false
	}

	// Bit-cost ratio (matches Vortex DictScheme). All costs are in bits,
	// avoiding header-size accounting and wrapper allocations. May over-
	// estimate for very small arrays where per-node header overhead
	// dominates, but headers are noise at scale.
	elemBitWidth := uint64(array.PTypeForType[T]().ByteWidth()) * 8
	valuesCost := elemBitWidth * distinctCount
	codesBW := uint64(bitWidthForUnsigned(distinctCount - 1))
	codesCost := codesBW * n

	if avgRunLength >= 4 {
		runCount := uint64(float64(n)/avgRunLength + 0.5)
		if runCount == 0 {
			runCount = 1
		}
		// Assume codes may be RLE-compressed: codes bitpacked + 32-bit run ends.
		rleCost := (codesBW + 32) * runCount
		if rleCost < codesCost {
			codesCost = rleCost
		}
	}

	before := n * elemBitWidth
	after := valuesCost + codesCost
	if after >= before {
		return 0, false
	}
	return float64(before) / float64(after), true
}

func estimateStringDict[S statsSource[string]](stats S, ctx planContext, estimatedDistinctCount uint64) (float64, bool) {
	n := stats.Source().Length()
	if ctx.depth <= 0 || n == 0 || estimatedDistinctCount > n/2 {
		return 0, false
	}
	return estimateBySample(stats, ctx, buildStringDictArray[string])
}

func estimateFloatDict[T Float, S statsSource[T]](stats S, ctx planContext, distinctCount uint64, valueCount uint64) (float64, bool) {
	if ctx.depth <= 0 || valueCount == 0 || distinctCount > valueCount/2 {
		return 0, false
	}
	return estimateBySample(stats, ctx, func(arr array.ArrayCore[T], ctx planContext) (EncodedArray[T], error) {
		return buildFloatDictFromDistinct(arr, nil, ctx)
	})
}
