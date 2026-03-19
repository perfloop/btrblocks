package btrblocks

import (
	"fmt"
	"io"
	"math"

	"github.com/axiomhq/btrblocks/array"
)

type dictCodec[T Integer | Float | String, U UnsignedInteger] struct {
	values  Codec[T]
	indices Codec[U]
}

func (d *dictCodec[T, U]) Kind() CodeType { return CodecTypeDict }
func (d *dictCodec[T, U]) Length() uint64 { return d.indices.Length() }
func (d *dictCodec[T, U]) PType() PType   { return pTypeForType[T]() }

func (d *dictCodec[T, U]) BinarySize() uint64 {
	return uint64(headerSize) + d.values.BinarySize() + d.indices.BinarySize()
}

func (d *dictCodec[T, U]) ValueAt(offset uint64) T {
	return d.values.ValueAt(uint64(d.indices.ValueAt(offset)))
}

func (d *dictCodec[T, U]) Decode(dst []T) error {
	if err := validateDecodeLength(d.indices.Length(), len(dst)); err != nil {
		return err
	}
	values := make([]T, d.values.Length())
	if err := d.values.Decode(values); err != nil {
		return err
	}
	indices := make([]U, d.indices.Length())
	if err := d.indices.Decode(indices); err != nil {
		return err
	}
	for i, index := range indices {
		dst[i] = values[int(index)]
	}
	return nil
}

func (d *dictCodec[T, U]) WriteTo(w io.Writer) (int64, error) {
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

func readDictCodec[T Integer | Float | String](r io.Reader, h header) (Codec[T], error) {
	if h.BodySize != 0 {
		return nil, fmt.Errorf("codec: dict body size = %d, want 0", h.BodySize)
	}
	values, err := readCodec[T](r)
	if err != nil {
		return nil, err
	}
	childHeader, err := readHeader(r)
	if err != nil {
		return nil, err
	}
	switch childHeader.ElemType {
	case PTypeUint8:
		indices, err := readCodecWithHeader[uint8](r, childHeader)
		if err != nil {
			return nil, err
		}
		if indices.Length() != h.Length {
			return nil, fmt.Errorf("codec: dict length = %d, want %d", h.Length, indices.Length())
		}
		return &dictCodec[T, uint8]{values: values, indices: indices}, nil
	case PTypeUint16:
		indices, err := readCodecWithHeader[uint16](r, childHeader)
		if err != nil {
			return nil, err
		}
		if indices.Length() != h.Length {
			return nil, fmt.Errorf("codec: dict length = %d, want %d", h.Length, indices.Length())
		}
		return &dictCodec[T, uint16]{values: values, indices: indices}, nil
	case PTypeUint32:
		indices, err := readCodecWithHeader[uint32](r, childHeader)
		if err != nil {
			return nil, err
		}
		if indices.Length() != h.Length {
			return nil, fmt.Errorf("codec: dict length = %d, want %d", h.Length, indices.Length())
		}
		return &dictCodec[T, uint32]{values: values, indices: indices}, nil
	case PTypeUint64:
		indices, err := readCodecWithHeader[uint64](r, childHeader)
		if err != nil {
			return nil, err
		}
		if indices.Length() != h.Length {
			return nil, fmt.Errorf("codec: dict length = %d, want %d", h.Length, indices.Length())
		}
		return &dictCodec[T, uint64]{values: values, indices: indices}, nil
	default:
		return nil, fmt.Errorf("codec: dict index type = %v, want unsigned integer", childHeader.ElemType)
	}
}

func buildIntegerDictCodec[T Integer](arr array.Array[T], ctx planContext) (Codec[T], error) {
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

	valuesCodec := newRawCodec(buildArray(values))
	return buildDictIndicesCodec(valuesCodec, indices, ctx)
}

func buildFloatDictCodec[T Float](arr array.Array[T], ctx planContext) (Codec[T], error) {
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

	valuesCodec, err := compressArray(buildArray(values), ctx.descend().withExcludes(CodecTypeDict))
	if err != nil {
		return nil, err
	}
	return buildDictIndicesCodec(valuesCodec, indices, ctx)
}

func buildStringDictCodec[T String](arr array.Array[T], ctx planContext) (Codec[T], error) {
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

	valuesCodec, err := compressArray(buildArray(values), ctx.descend().withExcludes(CodecTypeDict))
	if err != nil {
		return nil, err
	}
	return buildDictIndicesCodec(valuesCodec, indices, ctx)
}

func buildDictIndicesCodec[T Integer | Float | String](valuesCodec Codec[T], indices []uint64, ctx planContext) (Codec[T], error) {
	maxIndex := uint64(0)
	if len(indices) > 0 {
		for _, idx := range indices {
			if idx > maxIndex {
				maxIndex = idx
			}
		}
	}
	childCtx := ctx.descend().withExcludes(CodecTypeDict, CodecTypeSequence)
	switch {
	case maxIndex <= uint64(^uint8(0)):
		narrow := make([]uint8, len(indices))
		for i, idx := range indices {
			narrow[i] = uint8(idx)
		}
		indicesCodec, err := compressUnsignedArray(array.NewPrimitivesUnsafe(narrow), childCtx)
		if err != nil {
			return nil, err
		}
		return &dictCodec[T, uint8]{values: valuesCodec, indices: indicesCodec}, nil
	case maxIndex <= uint64(^uint16(0)):
		narrow := make([]uint16, len(indices))
		for i, idx := range indices {
			narrow[i] = uint16(idx)
		}
		indicesCodec, err := compressUnsignedArray(array.NewPrimitivesUnsafe(narrow), childCtx)
		if err != nil {
			return nil, err
		}
		return &dictCodec[T, uint16]{values: valuesCodec, indices: indicesCodec}, nil
	case maxIndex <= uint64(^uint32(0)):
		narrow := make([]uint32, len(indices))
		for i, idx := range indices {
			narrow[i] = uint32(idx)
		}
		indicesCodec, err := compressUnsignedArray(array.NewPrimitivesUnsafe(narrow), childCtx)
		if err != nil {
			return nil, err
		}
		return &dictCodec[T, uint32]{values: valuesCodec, indices: indicesCodec}, nil
	default:
		narrow := make([]uint64, len(indices))
		copy(narrow, indices)
		indicesCodec, err := compressUnsignedArray(array.NewPrimitivesUnsafe(narrow), childCtx)
		if err != nil {
			return nil, err
		}
		return &dictCodec[T, uint64]{values: valuesCodec, indices: indicesCodec}, nil
	}
}

func estimateIntegerDict[T Integer](distinctRatio float64) func(array.Array[T], planContext) (float64, bool) {
	return func(arr array.Array[T], ctx planContext) (float64, bool) {
		if ctx.depth <= 0 || distinctRatio > 0.5 {
			return 0, false
		}
		return estimateBySample(arr, ctx, buildIntegerDictCodec[T])
	}
}

func estimateStringDict[T String](distinctRatio float64) func(array.Array[T], planContext) (float64, bool) {
	return func(arr array.Array[T], ctx planContext) (float64, bool) {
		if ctx.depth <= 0 || distinctRatio > 0.5 {
			return 0, false
		}
		return estimateBySample(arr, ctx, buildStringDictCodec[T])
	}
}

func estimateFloatDict[T Float](distinctRatio float64) func(array.Array[T], planContext) (float64, bool) {
	return func(arr array.Array[T], ctx planContext) (float64, bool) {
		if ctx.depth <= 0 || distinctRatio > 0.5 {
			return 0, false
		}
		return estimateBySample(arr, ctx, buildFloatDictCodec[T])
	}
}
