package btrblocks

import (
	"io"
)

// compile-time type assertions
var (
	_ Codec[int8]    = (*DictCodec[int8])(nil)
	_ Codec[int16]   = (*DictCodec[int16])(nil)
	_ Codec[int32]   = (*DictCodec[int32])(nil)
	_ Codec[int64]   = (*DictCodec[int64])(nil)
	_ Codec[uint8]   = (*DictCodec[uint8])(nil)
	_ Codec[uint16]  = (*DictCodec[uint16])(nil)
	_ Codec[uint32]  = (*DictCodec[uint32])(nil)
	_ Codec[uint64]  = (*DictCodec[uint64])(nil)
	_ Codec[float32] = (*DictCodec[float32])(nil)
	_ Codec[float64] = (*DictCodec[float64])(nil)
	_ Codec[string]  = (*DictCodec[string])(nil)
)

type DictCodec[T Integer | Float | String] struct {
	values  Codec[T]
	indices Codec[uint64]
}

func newDictCodec[T Integer | Float | String](data []T, cmpFn cmpFn[T], depth int) (*DictCodec[T], error) {
	if depth <= 0 {
		return nil, errDepthExhausted
	}
	var (
		dict    = make(map[T]uint64)
		indices = make([]uint64, len(data))
		values  = make([]T, 0, len(data))
	)

	for i, val := range data {
		idx, ok := dict[val]
		if !ok {
			idx = uint64(len(values))
			dict[val] = idx
			values = append(values, val)
		}
		indices[i] = idx
	}

	valuesCodec := compress(values, depth-1)
	indicesCodec := CompressInteger(indices, depth-1)

	return &DictCodec[T]{values: valuesCodec, indices: indicesCodec}, nil
}

func NewDictFloatCodec[T Float](data []T, depth int) (*DictCodec[T], error) {
	return newDictCodec(data, cmpFloats[T], depth)
}

func NewDictIntegerCodec[T Integer](data []T, depth int) (*DictCodec[T], error) {
	return newDictCodec(data, cmpIntegers[T], depth)
}

func NewDictStringCodec[T String](data []T, depth int) (*DictCodec[T], error) {
	return newDictCodec(data, cmpStrings[T], depth)
}

func (d *DictCodec[T]) ValueAt(offset uint64) (T, error) {
	var zero T
	id, err := d.indices.ValueAt(offset)
	if err != nil {
		return zero, err
	}
	return d.values.ValueAt(id)
}

func (d *DictCodec[T]) WriteTo(w io.Writer) (n int64, err error) {
	n, err = d.values.WriteTo(w)
	if err != nil {
		return n, err
	}
	m, err := d.indices.WriteTo(w)
	return n + m, err
}

func (d *DictCodec[T]) Children() []Scheme { return []Scheme{d.values.(Scheme), d.indices.(Scheme)} }
func (d *DictCodec[T]) BinarySize() uint64 { return d.values.BinarySize() + d.indices.BinarySize() }
func (d *DictCodec[T]) Length() uint64     { return d.indices.Length() }
func (d *DictCodec[T]) PType() PType       { return pTypeForType[T]() }
