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

func (d *dictArray[T]) CopyTo(dst []T) error {
	if err := validateCopyLength(d.indices.Length(), len(dst)); err != nil {
		return err
	}
	values := make([]T, d.values.Length())
	if err := d.values.CopyTo(values); err != nil {
		return err
	}
	indices := make([]uint64, d.indices.Length())
	if err := d.indices.CopyToU64(indices); err != nil {
		return err
	}
	for i, index := range indices {
		dst[i] = values[int(index)]
	}
	return nil
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

func buildIntegerDictArray[T Integer](arr array.Array[T], ctx planContext) (EncodedArray[T], error) {
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

	valuesCodec := newRawArray(buildArray(values))
	return buildDictIndicesArray(valuesCodec, indices, ctx)
}

func buildFloatDictArray[T Float](arr array.Array[T], ctx planContext) (EncodedArray[T], error) {
	if ctx.depth <= 0 {
		return nil, errDepthExhausted
	}
	dict := make(map[uint64]uint64)
	values := make([]T, 0, arr.Length())
	indices := make([]uint64, arr.Length())
	for i := uint64(0); i < arr.Length(); i++ {
		value := arr.ValueAt(i)
		key := floatDictKey(value)
		index, ok := dict[key]
		if !ok {
			index = uint64(len(values))
			dict[key] = index
			values = append(values, value)
		}
		indices[i] = index
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
		before := rawBinarySize(stats.Source())
		if after >= before {
			return 0, false
		}
		return float64(before) / float64(after), true
	}
}

func estimateStringDict[S statsSource[string]](distinctRatio float64) func(S, planContext) (float64, bool) {
	return func(stats S, ctx planContext) (float64, bool) {
		if ctx.depth <= 0 || distinctRatio > 0.5 {
			return 0, false
		}
		return estimateBySample(stats, ctx, buildStringDictArray[string])
	}
}

func estimateFloatDict[T Float, S statsSource[T]](distinctRatio float64) func(S, planContext) (float64, bool) {
	return func(stats S, ctx planContext) (float64, bool) {
		if ctx.depth <= 0 || distinctRatio > 0.5 {
			return 0, false
		}
		return estimateBySample(stats, ctx, buildFloatDictArray[T])
	}
}
