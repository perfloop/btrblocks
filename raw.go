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

// encodeRaw wraps a materialized array without secondary compression.
func encodeRaw[T Integer | Float | String](arr array.Array[T]) EncodedArray[T] {
	return newRawArray(arr)
}

func (r *rawArray[T]) CodecType() CodecType          { return CodecTypeRaw }
func (r *rawArray[T]) Length() uint64                { return r.arr.Length() }
func (r *rawArray[T]) IsValid(offset uint64) bool    { return r.arr.IsValid(offset) }
func (r *rawArray[T]) NullCount() uint64             { return r.arr.NullCount() }
func (r *rawArray[T]) PType() PType                  { return r.arr.PType() }
func (r *rawArray[T]) BinarySize() uint64            { return uint64(headerSize) + r.arr.BinarySize() }
func (r *rawArray[T]) DecodedBytes() (uint64, error) { return decodedBytesFor(r.Length(), r.PType()) }

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

func (r *rawArray[T]) Slice(start, end uint64) (EncodedArray[T], error) {
	sliced, err := r.arr.Slice(start, end)
	if err != nil {
		return nil, err
	}
	return newRawArray(sliced), nil
}

func (r *rawArray[T]) WriteTo(w io.Writer) (int64, error) {
	n, err := codecHeader{
		Version:  versionNumber,
		Type:     CodecTypeRaw,
		ElemType: r.arr.PType(),
		Length:   r.arr.Length(),
		NumBytes: r.arr.BinarySize(),
	}.WriteTo(w)
	if err != nil {
		return n, err
	}
	nn, err := r.arr.WriteTo(w)
	return n + int64(nn), err
}

func readRawArray[T Integer | Float | String](br *array.BufReader, h codecHeader, opts ReadOptions, readBody arrayBodyReader[T]) (EncodedArray[T], error) {
	arr, err := readBody(br, opts)
	if err != nil {
		return nil, fmt.Errorf("codec: raw body: %w", err)
	}
	if h.Length != arr.Length() {
		return nil, fmt.Errorf("codec: raw length = %d, want %d", arr.Length(), h.Length)
	}
	if h.NumBytes != arr.BinarySize() {
		return nil, fmt.Errorf("codec: raw body size = %d, want %d", arr.BinarySize(), h.NumBytes)
	}
	return &rawArray[T]{arr: arr}, nil
}
