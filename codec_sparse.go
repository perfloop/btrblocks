package btrblocks

import (
	"fmt"
	"io"

	"github.com/axiomhq/btrblocks/array"
)

type sparseCodec[T Integer | Float | String, U UnsignedInteger] struct {
	length  uint64
	filler  T
	values  Codec[T]
	offsets Codec[U]
}

func (s *sparseCodec[T, U]) Kind() CodeType { return CodecTypeSparse }
func (s *sparseCodec[T, U]) Length() uint64 { return s.length }
func (s *sparseCodec[T, U]) PType() PType   { return pTypeForType[T]() }

func (s *sparseCodec[T, U]) BinarySize() uint64 {
	return uint64(headerSize) + constBodyBinarySize(s.filler) + s.values.BinarySize() + s.offsets.BinarySize()
}

func (s *sparseCodec[T, U]) ValueAt(offset uint64) T {
	if offset >= s.length {
		panic(errOffsetOutOfRange)
	}
	lo, hi := uint64(0), s.offsets.Length()
	for lo < hi {
		mid := lo + (hi-lo)/2
		value := uint64(s.offsets.ValueAt(mid))
		if value < offset {
			lo = mid + 1
		} else if value > offset {
			hi = mid
		} else {
			return s.values.ValueAt(mid)
		}
	}
	return s.filler
}

func (s *sparseCodec[T, U]) Decode(dst []T) error {
	if err := validateDecodeLength(s.length, len(dst)); err != nil {
		return err
	}
	for i := range dst {
		dst[i] = s.filler
	}
	values := make([]T, s.values.Length())
	if err := s.values.Decode(values); err != nil {
		return err
	}
	offsets := make([]U, s.offsets.Length())
	if err := s.offsets.Decode(offsets); err != nil {
		return err
	}
	for i, off := range offsets {
		dst[int(off)] = values[i]
	}
	return nil
}

func (s *sparseCodec[T, U]) WriteTo(w io.Writer) (int64, error) {
	body := constBodyArray(s.filler)
	n, err := header{
		Version:  versionNumber,
		Kind:     CodecTypeSparse,
		ElemType: pTypeForType[T](),
		Length:   s.length,
		BodySize: body.BinarySize(),
	}.WriteTo(w)
	if err != nil {
		return n, err
	}
	nn, err := body.WriteTo(w)
	n += int64(nn)
	if err != nil {
		return n, err
	}
	nn, err = s.values.WriteTo(w)
	n += nn
	if err != nil {
		return n, err
	}
	nn, err = s.offsets.WriteTo(w)
	return n + nn, err
}

func readSparseCodec[T Integer | Float | String](r io.Reader, h header) (Codec[T], error) {
	if h.Length == 0 {
		return nil, fmt.Errorf("codec: sparse length = 0")
	}
	fillerArr, err := array.ReadArray[T](r)
	if err != nil {
		return nil, err
	}
	if fillerArr.Length() != 1 {
		return nil, fmt.Errorf("codec: sparse filler length = %d, want 1", fillerArr.Length())
	}
	if fillerArr.BinarySize() != h.BodySize {
		return nil, fmt.Errorf("codec: sparse body size = %d, want %d", h.BodySize, fillerArr.BinarySize())
	}
	filler := fillerArr.ValueAt(0)
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
		offsets, err := readCodecWithHeader[uint8](r, childHeader)
		if err != nil {
			return nil, err
		}
		if values.Length() != offsets.Length() {
			return nil, fmt.Errorf("codec: sparse values length = %d, offsets length = %d", values.Length(), offsets.Length())
		}
		return &sparseCodec[T, uint8]{length: h.Length, filler: filler, values: values, offsets: offsets}, nil
	case PTypeUint16:
		offsets, err := readCodecWithHeader[uint16](r, childHeader)
		if err != nil {
			return nil, err
		}
		if values.Length() != offsets.Length() {
			return nil, fmt.Errorf("codec: sparse values length = %d, offsets length = %d", values.Length(), offsets.Length())
		}
		return &sparseCodec[T, uint16]{length: h.Length, filler: filler, values: values, offsets: offsets}, nil
	case PTypeUint32:
		offsets, err := readCodecWithHeader[uint32](r, childHeader)
		if err != nil {
			return nil, err
		}
		if values.Length() != offsets.Length() {
			return nil, fmt.Errorf("codec: sparse values length = %d, offsets length = %d", values.Length(), offsets.Length())
		}
		return &sparseCodec[T, uint32]{length: h.Length, filler: filler, values: values, offsets: offsets}, nil
	case PTypeUint64:
		offsets, err := readCodecWithHeader[uint64](r, childHeader)
		if err != nil {
			return nil, err
		}
		if values.Length() != offsets.Length() {
			return nil, fmt.Errorf("codec: sparse values length = %d, offsets length = %d", values.Length(), offsets.Length())
		}
		return &sparseCodec[T, uint64]{length: h.Length, filler: filler, values: values, offsets: offsets}, nil
	default:
		return nil, fmt.Errorf("codec: sparse offset type = %v, want unsigned integer", childHeader.ElemType)
	}
}

func buildSparseCodec[T Integer | Float | String](arr array.Array[T], ctx planContext, filler T, cmp cmpFn[T]) (Codec[T], error) {
	var zero T
	switch any(zero).(type) {
	case float32, float64, string:
		return buildSparseCodecWithValues(arr, ctx, filler, cmp, false)
	default:
		return buildSparseCodecWithValues(arr, ctx, filler, cmp, true)
	}
}

func buildSparseCodecWithValues[T Integer | Float | String](arr array.Array[T], ctx planContext, filler T, cmp cmpFn[T], compressValues bool) (Codec[T], error) {
	if ctx.depth <= 0 {
		return nil, errDepthExhausted
	}
	if arr.Length() == 0 {
		return nil, errDataEmpty
	}

	values := make([]T, 0)
	offsets := make([]uint64, 0)
	for i := uint64(0); i < arr.Length(); i++ {
		value := arr.ValueAt(i)
		if !cmp(value, filler) {
			values = append(values, value)
			offsets = append(offsets, i)
		}
	}

	var valuesCodec Codec[T]
	if compressValues {
		var err error
		valuesCodec, err = compressArray(buildArray(values), ctx.descend().withExcludes(CodecTypeSparse, CodecTypeDict))
		if err != nil {
			return nil, err
		}
	} else {
		valuesCodec = newRawCodec(buildArray(values))
	}
	childCtx := ctx.descend().withExcludes(CodecTypeSparse, CodecTypeDict)
	maxOffset := uint64(0)
	if len(offsets) > 0 {
		maxOffset = offsets[len(offsets)-1]
	}
	switch {
	case maxOffset <= uint64(^uint8(0)):
		narrow := make([]uint8, len(offsets))
		for i, off := range offsets {
			narrow[i] = uint8(off)
		}
		offsetsCodec, err := compressUnsignedArray(array.NewPrimitivesUnsafe(narrow), childCtx)
		if err != nil {
			return nil, err
		}
		return &sparseCodec[T, uint8]{length: arr.Length(), filler: filler, values: valuesCodec, offsets: offsetsCodec}, nil
	case maxOffset <= uint64(^uint16(0)):
		narrow := make([]uint16, len(offsets))
		for i, off := range offsets {
			narrow[i] = uint16(off)
		}
		offsetsCodec, err := compressUnsignedArray(array.NewPrimitivesUnsafe(narrow), childCtx)
		if err != nil {
			return nil, err
		}
		return &sparseCodec[T, uint16]{length: arr.Length(), filler: filler, values: valuesCodec, offsets: offsetsCodec}, nil
	case maxOffset <= uint64(^uint32(0)):
		narrow := make([]uint32, len(offsets))
		for i, off := range offsets {
			narrow[i] = uint32(off)
		}
		offsetsCodec, err := compressUnsignedArray(array.NewPrimitivesUnsafe(narrow), childCtx)
		if err != nil {
			return nil, err
		}
		return &sparseCodec[T, uint32]{length: arr.Length(), filler: filler, values: valuesCodec, offsets: offsetsCodec}, nil
	default:
		narrow := make([]uint64, len(offsets))
		copy(narrow, offsets)
		offsetsCodec, err := compressUnsignedArray(array.NewPrimitivesUnsafe(narrow), childCtx)
		if err != nil {
			return nil, err
		}
		return &sparseCodec[T, uint64]{length: arr.Length(), filler: filler, values: valuesCodec, offsets: offsetsCodec}, nil
	}
}

func estimateSparse[T Integer | Float | String](isConst bool, topCount uint64, filler T, cmp cmpFn[T]) func(array.Array[T], planContext) (float64, bool) {
	return func(arr array.Array[T], ctx planContext) (float64, bool) {
		if ctx.depth <= 0 || isConst || float64(topCount)/float64(arr.Length()) < 0.9 {
			return 0, false
		}
		return estimateBySample(arr, ctx, func(arr array.Array[T], ctx planContext) (Codec[T], error) {
			return buildSparseCodec(arr, ctx, filler, cmp)
		})
	}
}
