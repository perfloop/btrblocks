package btrblocks

import (
	"fmt"
	"io"

	"github.com/axiomhq/btrblocks/array"
)

type rawCodec[T Integer | Float | String] struct {
	arr array.Array[T]
}

func newRawCodec[T Integer | Float | String](arr array.Array[T]) *rawCodec[T] {
	return &rawCodec[T]{arr: arr}
}

func (r *rawCodec[T]) Kind() CodeType     { return CodecTypeRaw }
func (r *rawCodec[T]) Length() uint64     { return r.arr.Length() }
func (r *rawCodec[T]) PType() PType       { return pTypeForType[T]() }
func (r *rawCodec[T]) BinarySize() uint64 { return uint64(headerSize) + r.arr.BinarySize() }

func (r *rawCodec[T]) ValueAt(offset uint64) T {
	if offset >= r.arr.Length() {
		panic(errOffsetOutOfRange)
	}
	return r.arr.ValueAt(offset)
}

func (r *rawCodec[T]) Decode(dst []T) error {
	if err := validateDecodeLength(r.arr.Length(), len(dst)); err != nil {
		return err
	}
	r.arr.CopyTo(dst)
	return nil
}

func (r *rawCodec[T]) WriteTo(w io.Writer) (int64, error) {
	n, err := header{
		Version:  versionNumber,
		Kind:     CodecTypeRaw,
		ElemType: pTypeForType[T](),
		Length:   r.arr.Length(),
		BodySize: r.arr.BinarySize(),
	}.WriteTo(w)
	if err != nil {
		return n, err
	}
	nn, err := r.arr.WriteTo(w)
	return n + int64(nn), err
}

func readRawCodec[T Integer | Float | String](r io.Reader, h header) (Codec[T], error) {
	arr, err := array.ReadArray[T](r)
	if err != nil {
		return nil, err
	}
	if h.Length != arr.Length() {
		return nil, fmt.Errorf("codec: raw length = %d, want %d", h.Length, arr.Length())
	}
	if h.BodySize != arr.BinarySize() {
		return nil, fmt.Errorf("codec: raw body size = %d, want %d", h.BodySize, arr.BinarySize())
	}
	return &rawCodec[T]{arr: arr}, nil
}
