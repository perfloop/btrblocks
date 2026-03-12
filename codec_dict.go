package btrblocks

import (
	"io"

	"github.com/axiomhq/btrblocks/array"
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

func newDictCodec[T Integer | Float | String](arr array.Array[T], cmpFn cmpFn[T], depth int) (*DictCodec[T], error) {
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

	valuesCodec := compress(values, depth-1)
	indicesCodec := CompressInteger(array.NewPrimitivesUnsafe[uint64](indices), depth-1)

	return &DictCodec[T]{values: valuesCodec, indices: indicesCodec}, nil
}

func NewDictFloatCodec[T Float](arr array.Array[T], depth int) (*DictCodec[T], error) {
	return newDictCodec(arr, cmpFloats[T], depth)
}

func NewDictIntegerCodec[T Integer](arr array.Array[T], depth int) (*DictCodec[T], error) {
	return newDictCodec(arr, cmpIntegers[T], depth)
}

func NewDictStringCodec[T String](arr array.Array[T], depth int) (*DictCodec[T], error) {
	return newDictCodec(arr, cmpStrings[T], depth)
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
