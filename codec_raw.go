package btrblocks

import (
	"fmt"
	"io"

	"github.com/axiomhq/btrblocks/array"
)

// rawArray wraps an array body without applying any secondary compression.
type rawArray[T Integer | Float | String] struct {
	arr array.Array[T]
}

func newRawArray[T Integer | Float | String](arr array.Array[T]) *rawArray[T] {
	return &rawArray[T]{arr: arr}
}

func (r *rawArray[T]) Encoding() CodeType { return CodecTypeRaw }
func (r *rawArray[T]) Length() uint64     { return r.arr.Length() }
func (r *rawArray[T]) PType() PType       { return pTypeForType[T]() }
func (r *rawArray[T]) BinarySize() uint64 { return uint64(headerSize) + r.arr.BinarySize() }

func (r *rawArray[T]) ValueAt(offset uint64) T {
	if offset >= r.arr.Length() {
		panic(errOffsetOutOfRange)
	}
	return r.arr.ValueAt(offset)
}

func (r *rawArray[T]) DecompressInto(dst []T) error {
	if err := checkDstLen(dst, r.arr.Length()); err != nil {
		return err
	}
	r.arr.CopyTo(dst[:r.arr.Length()])
	return nil
}

func (r *rawArray[T]) Decompress() ([]T, error) {
	dst := make([]T, r.arr.Length())
	return dst, r.DecompressInto(dst)
}

func (r *rawArray[T]) Slice(start, end uint64) (EncodedArray[T], error) {
	return sliceToRawArray(r, start, end)
}

func (r *rawArray[T]) WriteTo(w io.Writer) (int64, error) {
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

func readRawArray[T Integer | Float | String](r io.Reader, h header) (EncodedArray[T], error) {
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
	return &rawArray[T]{arr: arr}, nil
}
