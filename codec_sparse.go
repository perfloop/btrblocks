package btrblocks

import (
	"fmt"
	"io"

	"github.com/axiomhq/btrblocks/array"
)

// SparseCodec stores a dominant filler value once, plus the non-filler values
// and their offsets. Effective when one value dominates ≥90% of the array.
type SparseCodec[T Integer | Float | String, U UnsignedInteger] struct {
	length  uint64
	filler  T
	values  Codec[T] // non-filler values
	offsets Codec[U] // positions of non-filler values
}

func newSparseCodecFromSource[T Integer | Float | String](
	arr array.Array[T], cmpFn cmpFn[T], depth int, excludes codecExcludes,
) (Codec[T], error) {
	if depth <= 0 {
		return nil, errDepthExhausted
	}
	if arr.Length() == 0 {
		return nil, errDataEmpty
	}

	// Find the most frequent value.
	counts := make(map[any]uint64, 256)
	for i := uint64(0); i < arr.Length(); i++ {
		counts[any(arr.ValueAt(i))]++
	}
	var (
		fillerKey   any
		fillerCount uint64
	)
	for k, c := range counts {
		if c > fillerCount {
			fillerKey = k
			fillerCount = c
		}
	}
	filler := fillerKey.(T)

	// Collect non-filler values and their offsets.
	var (
		nonFiller = make([]T, 0, arr.Length()-fillerCount)
		offsets   = make([]uint64, 0, arr.Length()-fillerCount)
	)
	for i := uint64(0); i < arr.Length(); i++ {
		if !cmpFn(arr.ValueAt(i), filler) {
			nonFiller = append(nonFiller, arr.ValueAt(i))
			offsets = append(offsets, i)
		}
	}

	maxOffset := uint64(0)
	if len(offsets) > 0 {
		maxOffset = offsets[len(offsets)-1]
	}
	switch {
	case maxOffset <= uint64(^uint8(0)):
		return newSparseCodecWithWidth[T, uint8](arr.Length(), filler, nonFiller, offsets, depth, excludes)
	case maxOffset <= uint64(^uint16(0)):
		return newSparseCodecWithWidth[T, uint16](arr.Length(), filler, nonFiller, offsets, depth, excludes)
	case maxOffset <= uint64(^uint32(0)):
		return newSparseCodecWithWidth[T, uint32](arr.Length(), filler, nonFiller, offsets, depth, excludes)
	default:
		return newSparseCodecWithWidth[T, uint64](arr.Length(), filler, nonFiller, offsets, depth, excludes)
	}
}

func newSparseCodecWithWidth[T Integer | Float | String, U UnsignedInteger](
	length uint64, filler T, values []T, offsets []uint64, depth int, excludes codecExcludes,
) (Codec[T], error) {
	narrow := make([]U, len(offsets))
	for i, off := range offsets {
		narrow[i] = U(off)
	}
	childExcl := excludes.with(CodecTypeSparse, CodecTypeDict)
	valuesCodec := compress(values, depth-1, childExcl)
	offsetsCodec := CompressInteger(array.NewPrimitivesUnsafe(narrow), depth-1, childExcl)
	return &SparseCodec[T, U]{length: length, filler: filler, values: valuesCodec, offsets: offsetsCodec}, nil
}

func NewSparseIntegerCodec[T Integer](arr array.Array[T], depth int, excludes codecExcludes) (Codec[T], error) {
	return newSparseCodecFromSource(arr, cmpIntegers[T], depth, excludes)
}

func NewSparseFloatCodec[T Float](arr array.Array[T], depth int, excludes codecExcludes) (Codec[T], error) {
	return newSparseCodecFromSource(arr, cmpFloats[T], depth, excludes)
}

func NewSparseStringCodec[T String](arr array.Array[T], depth int, excludes codecExcludes) (Codec[T], error) {
	return newSparseCodecFromSource(arr, cmpStrings[T], depth, excludes)
}

func (s *SparseCodec[T, U]) ValueAt(offset uint64) (T, error) {
	if offset >= s.length {
		var zero T
		return zero, errOffsetOutOfRange
	}
	// Binary search for offset in the offsets array.
	lo, hi := uint64(0), s.offsets.Length()
	for lo < hi {
		mid := lo + (hi-lo)/2
		off, err := s.offsets.ValueAt(mid)
		if err != nil {
			var zero T
			return zero, err
		}
		if uint64(off) < offset {
			lo = mid + 1
		} else if uint64(off) > offset {
			hi = mid
		} else {
			return s.values.ValueAt(mid)
		}
	}
	return s.filler, nil
}

func (s *SparseCodec[T, U]) Decode(dst []T) error {
	if err := validateDecodeLength(s.length, len(dst)); err != nil {
		return err
	}
	// Fill with filler value.
	for i := range dst {
		dst[i] = s.filler
	}
	// Scatter non-filler values.
	values := make([]T, s.values.Length())
	if err := s.values.Decode(values); err != nil {
		return err
	}
	offsets := make([]U, s.offsets.Length())
	if err := s.offsets.Decode(offsets); err != nil {
		return err
	}
	for i, off := range offsets {
		dst[off] = values[i]
	}
	return nil
}

func (s *SparseCodec[T, U]) Children() []Scheme {
	return []Scheme{s.values.(Scheme), s.offsets.(Scheme)}
}
func (s *SparseCodec[T, U]) Length() uint64 { return s.length }
func (s *SparseCodec[T, U]) PType() PType   { return pTypeForType[T]() }

func (s *SparseCodec[T, U]) BinarySize() uint64 {
	return uint64(headerSize) + constBodyBinarySize(s.filler) + s.values.BinarySize() + s.offsets.BinarySize()
}

func (s *SparseCodec[T, U]) WriteTo(w io.Writer) (n int64, err error) {
	fillerBody := constBodyArray(s.filler)
	n, err = Header{
		Version:    1,
		Kind:       CodecTypeSparse,
		ElemType:   pTypeForType[T](),
		ChildCount: 2,
		Flags:      0,
		Length:     s.length,
		BodySize:   fillerBody.BinarySize(),
	}.WriteTo(w)
	if err != nil {
		return n, err
	}

	nn, err := fillerBody.WriteTo(w)
	if err != nil {
		return n + int64(nn), err
	}
	n += int64(nn)

	nn, err = s.values.WriteTo(w)
	if err != nil {
		return n + int64(nn), err
	}
	n += int64(nn)

	nn, err = s.offsets.WriteTo(w)
	return n + int64(nn), err
}

func readSparseCodecWithOffsets[T Integer | Float | String, U UnsignedInteger](
	r io.Reader, header Header, filler T, values Codec[T], childHeader Header,
) (Codec[T], error) {
	offsets, err := readCodecWithHeader[U](r, childHeader)
	if err != nil {
		return nil, err
	}
	if values.Length() != offsets.Length() {
		return nil, fmt.Errorf("codec: sparse values length = %d, offsets length = %d", values.Length(), offsets.Length())
	}
	return &SparseCodec[T, U]{length: header.Length, filler: filler, values: values, offsets: offsets}, nil
}

func readSparseCodec[T Integer | Float | String](r io.Reader, header Header) (Codec[T], error) {
	if header.ChildCount != 2 {
		return nil, fmt.Errorf("codec: sparse child count = %d, want 2", header.ChildCount)
	}
	if header.Length == 0 {
		return nil, fmt.Errorf("codec: sparse length = 0")
	}

	// Read filler from body as a 1-element array.
	fillerArr, err := array.ReadArray[T](r)
	if err != nil {
		return nil, err
	}
	if fillerArr.Length() != 1 {
		return nil, fmt.Errorf("codec: sparse filler length = %d, want 1", fillerArr.Length())
	}
	filler := fillerArr.ValueAt(0)

	// Read values child.
	values, err := readCodec[T](r)
	if err != nil {
		return nil, err
	}

	// Read offsets child.
	childHeader, err := readHeader(r)
	if err != nil {
		return nil, err
	}
	switch childHeader.ElemType {
	case PTypeUint8:
		return readSparseCodecWithOffsets[T, uint8](r, header, filler, values, childHeader)
	case PTypeUint16:
		return readSparseCodecWithOffsets[T, uint16](r, header, filler, values, childHeader)
	case PTypeUint32:
		return readSparseCodecWithOffsets[T, uint32](r, header, filler, values, childHeader)
	case PTypeUint64:
		return readSparseCodecWithOffsets[T, uint64](r, header, filler, values, childHeader)
	default:
		return nil, fmt.Errorf("codec: sparse offset element type = %v, want unsigned integer", childHeader.ElemType)
	}
}
