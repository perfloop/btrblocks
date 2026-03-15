package btrblocks

import (
	"fmt"
	"io"
	"math"

	"github.com/axiomhq/btrblocks/array"
)

type DictCodec[T Integer | Float | String, U UnsignedInteger] struct {
	values  Codec[T]
	indices Codec[U]
}

func newDictCodecWithWidth[T Integer | Float | String, U UnsignedInteger](values []T, indices []uint64, depth int) (Codec[T], error) {
	narrow := make([]U, len(indices))
	for i, index := range indices {
		narrow[i] = U(index)
	}
	valuesCodec := compress(values, depth-1)
	indicesCodec := CompressInteger(array.NewPrimitivesUnsafe(narrow), depth-1)
	return &DictCodec[T, U]{values: valuesCodec, indices: indicesCodec}, nil
}

func newDictCodec[T Integer | String](arr array.Array[T], depth int) (Codec[T], error) {
	if depth <= 0 {
		return nil, errDepthExhausted
	}
	var (
		dict    = make(map[T]uint64)
		indices = make([]uint64, arr.Length())
		values  = make([]T, 0, arr.Length())
	)

	for i := uint64(0); i < arr.Length(); i++ {
		val := arr.ValueAt(i)
		idx, ok := dict[val]
		if !ok {
			idx = uint64(len(values))
			dict[val] = idx
			values = append(values, val)
		}
		indices[i] = idx
	}

	maxIndex := uint64(0)
	if len(values) > 0 {
		maxIndex = uint64(len(values) - 1)
	}
	switch {
	case maxIndex <= uint64(^uint8(0)):
		return newDictCodecWithWidth[T, uint8](values, indices, depth)
	case maxIndex <= uint64(^uint16(0)):
		return newDictCodecWithWidth[T, uint16](values, indices, depth)
	case maxIndex <= uint64(^uint32(0)):
		return newDictCodecWithWidth[T, uint32](values, indices, depth)
	default:
		return newDictCodecWithWidth[T, uint64](values, indices, depth)
	}
}

func NewDictFloatCodec[T Float](arr array.Array[T], depth int) (Codec[T], error) {
	if depth <= 0 {
		return nil, errDepthExhausted
	}
	var (
		dict    = make(map[uint64]uint64)
		indices = make([]uint64, arr.Length())
		values  = make([]T, 0, arr.Length())
	)

	for i := uint64(0); i < arr.Length(); i++ {
		val := arr.ValueAt(i)
		key := floatDictKey(val)
		idx, ok := dict[key]
		if !ok {
			idx = uint64(len(values))
			dict[key] = idx
			values = append(values, val)
		}
		indices[i] = idx
	}

	maxIndex := uint64(0)
	if len(values) > 0 {
		maxIndex = uint64(len(values) - 1)
	}
	switch {
	case maxIndex <= uint64(^uint8(0)):
		return newDictCodecWithWidth[T, uint8](values, indices, depth)
	case maxIndex <= uint64(^uint16(0)):
		return newDictCodecWithWidth[T, uint16](values, indices, depth)
	case maxIndex <= uint64(^uint32(0)):
		return newDictCodecWithWidth[T, uint32](values, indices, depth)
	default:
		return newDictCodecWithWidth[T, uint64](values, indices, depth)
	}
}

func NewDictIntegerCodec[T Integer](arr array.Array[T], depth int) (Codec[T], error) {
	return newDictCodec(arr, depth)
}

func NewDictStringCodec[T String](arr array.Array[T], depth int) (Codec[T], error) {
	return newDictCodec(arr, depth)
}

func (d *DictCodec[T, U]) ValueAt(offset uint64) (T, error) {
	var zero T
	id, err := d.indices.ValueAt(offset)
	if err != nil {
		return zero, err
	}
	return d.values.ValueAt(uint64(id))
}

func (d *DictCodec[T, U]) Decode(dst []T) error {
	values := make([]T, d.values.Length())
	if err := d.values.Decode(values); err != nil {
		return err
	}
	indices := make([]U, d.indices.Length())
	if err := d.indices.Decode(indices); err != nil {
		return err
	}
	for i, idx := range indices {
		dst[i] = values[idx]
	}
	return nil
}

func (d *DictCodec[T, U]) WriteTo(w io.Writer) (n int64, err error) {
	n, err = Header{
		Version:    1,
		Kind:       CodecTypeDict,
		ElemType:   pTypeForType[T](),
		ChildCount: 2,
		Flags:      0,
		Length:     d.indices.Length(),
		BodySize:   0,
	}.WriteTo(w)
	if err != nil {
		return n, err
	}

	nn, err := d.values.WriteTo(w)
	if err != nil {
		return n + int64(nn), err
	}
	n += int64(nn)

	nn, err = d.indices.WriteTo(w)
	return n + int64(nn), err
}

func (d *DictCodec[T, U]) Children() []Scheme { return []Scheme{d.values.(Scheme), d.indices.(Scheme)} }
func (d *DictCodec[T, U]) Length() uint64     { return d.indices.Length() }
func (d *DictCodec[T, U]) PType() PType       { return pTypeForType[T]() }

func (d *DictCodec[T, U]) BinarySize() uint64 {
	return uint64(headerSize) + d.values.BinarySize() + d.indices.BinarySize()
}

func floatDictKey[T Float](val T) uint64 {
	switch v := any(val).(type) {
	case float32:
		return uint64(math.Float32bits(v))
	case float64:
		return math.Float64bits(v)
	default:
		return 0
	}
}

func readDictCodecWithIndices[T Integer | Float | String, U UnsignedInteger](r io.Reader, header Header, values Codec[T], childHeader Header) (Codec[T], error) {
	indices, err := readCodecWithHeader[U](r, childHeader)
	if err != nil {
		return nil, err
	}
	if err := validateDictCodec(header.Length, values, indices); err != nil {
		return nil, err
	}
	return &DictCodec[T, U]{values: values, indices: indices}, nil
}

func validateDictCodec[T Integer | Float | String, U UnsignedInteger](length uint64, values Codec[T], indices Codec[U]) error {
	if indices.Length() != length {
		return fmt.Errorf("codec: dict length = %d, want %d", length, indices.Length())
	}
	decoded := make([]U, indices.Length())
	if err := indices.Decode(decoded); err != nil {
		return err
	}
	for i, index := range decoded {
		if uint64(index) >= values.Length() {
			return fmt.Errorf("codec: dict index %d = %d out of range for %d values", i, index, values.Length())
		}
	}
	return nil
}

func readDictCodec[T Integer | Float | String](r io.Reader, header Header) (Codec[T], error) {
	if header.ChildCount != 2 {
		return nil, fmt.Errorf("codec: dict child count = %d, want 2", header.ChildCount)
	}
	if header.BodySize != 0 {
		return nil, fmt.Errorf("codec: dict body size = %d, want 0", header.BodySize)
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
		return readDictCodecWithIndices[T, uint8](r, header, values, childHeader)
	case PTypeUint16:
		return readDictCodecWithIndices[T, uint16](r, header, values, childHeader)
	case PTypeUint32:
		return readDictCodecWithIndices[T, uint32](r, header, values, childHeader)
	case PTypeUint64:
		return readDictCodecWithIndices[T, uint64](r, header, values, childHeader)
	default:
		return nil, fmt.Errorf("codec: dict index element type = %v, want unsigned integer", childHeader.ElemType)
	}
}
