package btrblocks

import "io"

// compile-time type assertions
var (
	_ Codec[int8]    = (*RawCodec[int8])(nil)
	_ Codec[int16]   = (*RawCodec[int16])(nil)
	_ Codec[int32]   = (*RawCodec[int32])(nil)
	_ Codec[int64]   = (*RawCodec[int64])(nil)
	_ Codec[uint8]   = (*RawCodec[uint8])(nil)
	_ Codec[uint16]  = (*RawCodec[uint16])(nil)
	_ Codec[uint32]  = (*RawCodec[uint32])(nil)
	_ Codec[uint64]  = (*RawCodec[uint64])(nil)
	_ Codec[float32] = (*RawCodec[float32])(nil)
	_ Codec[float64] = (*RawCodec[float64])(nil)
	_ Codec[string]  = (*RawCodec[string])(nil)
)

type RawCodec[T Integer | Float | String] struct {
	data []T
}

func NewRawCodec[T Integer | Float | String](data []T) *RawCodec[T] {
	return &RawCodec[T]{data: data}
}

func (r *RawCodec[T]) Children() []Scheme {
	return nil
}

func (r *RawCodec[T]) ValueAt(offset uint64) (T, error) {
	var zero T
	if offset >= uint64(len(r.data)) {
		return zero, ErrOffsetOutOfRange
	}
	return r.data[offset], nil
}

func (r *RawCodec[T]) WriteTo(w io.Writer) (n int64, err error) {
	return int64(len(r.data)), nil
}

func (r *RawCodec[T]) BinarySize() uint64 {
	return uint64(len(r.data))
}

func (r *RawCodec[T]) Length() uint64 {
	return uint64(len(r.data))
}

func (r *RawCodec[T]) PType() PType {
	return pTypeForType[T]()
}
