package btrblocks

import (
	"fmt"
	"io"
	"math"

	"github.com/axiomhq/btrblocks/array"
)

// dictArray stores unique values plus an ordinal child that indexes into them.
type dictArray[T Integer | Float | String] struct {
	values  EncodedArray[T]
	indices ordinalArray
}

func (d *dictArray[T]) Encoding() CodeType { return CodecTypeDict }
func (d *dictArray[T]) Length() uint64     { return d.indices.Length() }
func (d *dictArray[T]) PType() PType       { return pTypeForType[T]() }

func (d *dictArray[T]) BinarySize() uint64 {
	return uint64(headerSize) + d.values.BinarySize() + d.indices.BinarySize()
}

func (d *dictArray[T]) ValueAt(offset uint64) T {
	return d.values.ValueAt(d.indices.ValueAt(offset))
}

func (d *dictArray[T]) DecompressInto(dst []T) error {
	if err := checkDstLen(dst, d.indices.Length()); err != nil {
		return err
	}
	values, err := d.values.Decompress()
	if err != nil {
		return err
	}
	indices, err := decompressOrdinals(d.indices)
	if err != nil {
		return err
	}
	for i, idx := range indices {
		dst[i] = values[idx]
	}
	return nil
}

func (d *dictArray[T]) Decompress() ([]T, error) {
	dst := make([]T, d.indices.Length())
	return dst, d.DecompressInto(dst)
}

func (d *dictArray[T]) Slice(start, end uint64) (EncodedArray[T], error) {
	indices, err := d.indices.Slice(start, end)
	if err != nil {
		return nil, err
	}
	return &dictArray[T]{values: d.values, indices: indices}, nil
}

func (d *dictArray[T]) WriteTo(w io.Writer) (int64, error) {
	n, err := header{
		Version:  versionNumber,
		Kind:     CodecTypeDict,
		ElemType: pTypeForType[T](),
		Length:   d.indices.Length(),
		BodySize: 0,
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

func floatDictKey[T Float](value T) uint64 {
	switch v := any(value).(type) {
	case float32:
		return uint64(math.Float32bits(v))
	case float64:
		return math.Float64bits(v)
	default:
		return 0
	}
}

func readDictArray[T Integer | Float | String](r io.Reader, h header) (EncodedArray[T], error) {
	if h.BodySize != 0 {
		return nil, fmt.Errorf("codec: dict body size = %d, want 0", h.BodySize)
	}
	values, err := readEncodedArray[T](r)
	if err != nil {
		return nil, err
	}
	childHeader, err := readHeader(r)
	if err != nil {
		return nil, err
	}
	indices, err := readOrdinalArray(r, childHeader)
	if err != nil {
		return nil, fmt.Errorf("codec: dict index %w", err)
	}
	if indices.Length() != h.Length {
		return nil, fmt.Errorf("codec: dict length = %d, want %d", h.Length, indices.Length())
	}
	return &dictArray[T]{values: values, indices: indices}, nil
}

// buildIntegerDictFromDistinct builds a dictionary-encoded array using
// pre-computed distinct values from stats when available. Falls back to
// scanning the array if distinct is empty (e.g. during sample estimation).
func buildIntegerDictFromDistinct[T Integer](arr array.Array[T], distinct intDistinctValues[T], ctx planContext) (EncodedArray[T], error) {
	if ctx.depth <= 0 {
		return nil, errDepthExhausted
	}

	var values []T
	var byKey map[T]uint64

	if len(distinct.byKey) > 0 {
		values = distinct.values
		byKey = distinct.byKey
	} else {
		byKey = make(map[T]uint64)
		for i := uint64(0); i < arr.Length(); i++ {
			value := arr.ValueAt(i)
			if _, ok := byKey[value]; !ok {
				byKey[value] = uint64(len(values))
				values = append(values, value)
			}
		}
	}

	indices := make([]uint64, arr.Length())
	for i := uint64(0); i < arr.Length(); i++ {
		indices[i] = byKey[arr.ValueAt(i)]
	}

	valuesCodec, err := compressArray(buildArray(values), ctx.descend().withIntegerExcludes(CodecTypeDict))
	if err != nil {
		return nil, err
	}
	return buildDictIndicesArray(valuesCodec, indices, ctx)
}

// buildFloatDictFromDistinct builds a dictionary-encoded array using
// pre-computed distinct values from stats, avoiding a redundant array scan.
// If distinct is empty (e.g. during sample estimation), it falls back to
// scanning the array.
func buildFloatDictFromDistinct[T Float](arr array.Array[T], distinct floatDistinctValues[T], ctx planContext) (EncodedArray[T], error) {
	if ctx.depth <= 0 {
		return nil, errDepthExhausted
	}

	var values []T
	var byKey map[uint64]uint64

	if len(distinct.byKey) > 0 {
		// Use pre-computed distinct values from stats.
		values = distinct.values
		byKey = distinct.byKey
	} else {
		// Fallback: scan the array (used during sample estimation).
		byKey = make(map[uint64]uint64)
		for i := uint64(0); i < arr.Length(); i++ {
			value := arr.ValueAt(i)
			key := floatDictKey(value)
			if _, ok := byKey[key]; !ok {
				byKey[key] = uint64(len(values))
				values = append(values, value)
			}
		}
	}

	// Build ordinal indices by scanning the array once.
	indices := make([]uint64, arr.Length())
	for i := uint64(0); i < arr.Length(); i++ {
		indices[i] = byKey[floatDictKey(arr.ValueAt(i))]
	}

	valuesCodec, err := compressArray(buildArray(values), ctx.descend().withFloatExcludes(CodecTypeDict))
	if err != nil {
		return nil, err
	}
	return buildDictIndicesArray(valuesCodec, indices, ctx)
}

func buildStringDictArray[T String](arr array.Array[T], ctx planContext) (EncodedArray[T], error) {
	if ctx.depth <= 0 {
		return nil, errDepthExhausted
	}
	dict := make(map[T]uint64)
	values := make([]T, 0, arr.Length())
	indices := make([]uint64, arr.Length())
	for i := uint64(0); i < arr.Length(); i++ {
		value := arr.ValueAt(i)
		index, ok := dict[value]
		if !ok {
			index = uint64(len(values))
			dict[value] = index
			values = append(values, value)
		}
		indices[i] = index
	}

	valuesCodec, err := compressArray(buildArray(values), ctx.descend().withStringExcludes(CodecTypeDict))
	if err != nil {
		return nil, err
	}
	return buildDictIndicesArray(valuesCodec, indices, ctx)
}

func buildDictIndicesArray[T Integer | Float | String](valuesCodec EncodedArray[T], indices []uint64, ctx planContext) (EncodedArray[T], error) {
	childCtx := ctx.descend().withIntegerExcludes(CodecTypeDict, CodecTypeSequence)
	indicesCodec, err := buildCompressedOrdinals(indices, childCtx, CodecTypeDict, CodecTypeSequence)
	if err != nil {
		return nil, err
	}
	return &dictArray[T]{values: valuesCodec, indices: indicesCodec}, nil
}

func estimateIntegerDict[T Integer, S statsSource[T]](distinctCount uint64, avgRunLength float64) func(S, planContext) (float64, bool) {
	return func(stats S, ctx planContext) (float64, bool) {
		if ctx.depth <= 0 {
			return 0, false
		}

		n := stats.Source().Length()
		if n == 0 || distinctCount <= 1 || distinctCount > n/2 {
			return 0, false
		}

		elemWidth := uint64(pTypeForType[T]().ByteWidth())
		valuesSize := uint64(headerSize) + uint64(primitiveArrayHeaderSize) + distinctCount*elemWidth

		codesWidth := bitWidthForUnsigned(distinctCount - 1)
		codesSize, ok := bitpackEncodedSize(n, codesWidth)
		if !ok {
			return 0, false
		}

		if avgRunLength >= 4 {
			runCount := uint64(float64(n)/avgRunLength + 0.5)
			if runCount == 0 {
				runCount = 1
			}
			runsSize, ok := bitpackEncodedSize(runCount, codesWidth)
			if ok {
				endsWidth := bitWidthForUnsigned(n - 1)
				endsSize, ok := bitpackEncodedSize(runCount-1, endsWidth)
				if ok {
					runEndSize := uint64(headerSize) + runsSize + endsSize
					if runEndSize < codesSize {
						codesSize = runEndSize
					}
				}
			}
		}

		after := uint64(headerSize) + valuesSize + codesSize
		before := newRawArray(stats.Source()).BinarySize()
		if after >= before {
			return 0, false
		}
		return float64(before) / float64(after), true
	}
}

func estimateStringDict[S statsSource[string]](estimatedDistinctCount uint64) func(S, planContext) (float64, bool) {
	return func(stats S, ctx planContext) (float64, bool) {
		n := stats.Source().Length()
		if ctx.depth <= 0 || n == 0 || estimatedDistinctCount > n/2 {
			return 0, false
		}
		return estimateBySample(stats, ctx, buildStringDictArray[string])
	}
}

func estimateFloatDict[T Float, S statsSource[T]](distinctRatio float64) func(S, planContext) (float64, bool) {
	return func(stats S, ctx planContext) (float64, bool) {
		if ctx.depth <= 0 || distinctRatio > distinctRatioThreshold {
			return 0, false
		}
		return estimateBySample(stats, ctx, func(arr array.Array[T], ctx planContext) (EncodedArray[T], error) {
			return buildFloatDictFromDistinct(arr, floatDistinctValues[T]{}, ctx)
		})
	}
}
