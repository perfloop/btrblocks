package codec

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/bits"

	"github.com/axiomhq/btrblocks/array"
)

const nullableBodySize = 8

// nullableArray composes a physical value encoding with a compressed bitmap
// of valid rows. Null payloads in values are canonical zero values; validity
// alone determines whether a row is null.
type nullableArray[T Integer | Float | String] struct {
	values    EncodedArray[T]
	validity  EncodedArray[uint8]
	nullCount uint64
}

func (a *nullableArray[T]) CodecType() CodecType { return CodecTypeNullable }
func (a *nullableArray[T]) Length() uint64       { return a.values.Length() }
func (a *nullableArray[T]) NullCount() uint64    { return a.nullCount }
func (a *nullableArray[T]) PType() PType         { return a.values.PType() }
func (a *nullableArray[T]) BinarySize() uint64 {
	return headerSize + nullableBodySize + a.values.BinarySize() + a.validity.BinarySize()
}
func (a *nullableArray[T]) DecodedBytes() (uint64, error) {
	return a.values.DecodedBytes()
}

func (a *nullableArray[T]) IsValid(offset uint64) bool {
	if offset >= a.Length() {
		panic(errOffsetOutOfRange)
	}
	return a.validity.ValueAt(offset>>3)&(uint8(1)<<(offset&7)) != 0
}

func (a *nullableArray[T]) ValueAt(offset uint64) T {
	if offset >= a.Length() {
		panic(errOffsetOutOfRange)
	}
	return a.values.ValueAt(offset)
}

func (a *nullableArray[T]) DecompressInto(dst []T) error {
	return a.values.DecompressInto(dst)
}

func (a *nullableArray[T]) Slice(start, end uint64) (EncodedArray[T], error) {
	if err := array.ValidateSliceBounds(a.Length(), start, end); err != nil {
		return nil, err
	}
	values, err := a.values.Slice(start, end)
	if err != nil {
		return nil, err
	}
	length := end - start
	bitmap, nullCount, err := array.ValidityBitmap(a, start, length)
	if err != nil {
		return nil, fmt.Errorf("codec: slice nullable validity: %w", err)
	}
	if nullCount == 0 {
		return values, nil
	}
	return &nullableArray[T]{
		values:    values,
		validity:  newRawArray(array.NewPrimitivesUnsafe(bitmap)),
		nullCount: nullCount,
	}, nil
}

func (a *nullableArray[T]) WriteTo(w io.Writer) (int64, error) {
	sum := writeSum{w: w}
	if err := sum.writeTo(codecHeader{
		Version:  versionNumber,
		Type:     CodecTypeNullable,
		ElemType: a.PType(),
		Length:   a.Length(),
		NumBytes: nullableBodySize,
	}); err != nil {
		return sum.n, err
	}
	var count [nullableBodySize]byte
	binary.LittleEndian.PutUint64(count[:], a.nullCount)
	if err := sum.write(count[:]); err != nil {
		return sum.n, err
	}
	if err := sum.writeTo(a.values); err != nil {
		return sum.n, err
	}
	if err := sum.writeTo(a.validity); err != nil {
		return sum.n, err
	}
	return sum.n, nil
}

func readNullableArray[T Integer | Float | String](br *array.BufReader, h codecHeader, opts ReadOptions, readValues encodedReader[T]) (EncodedArray[T], error) {
	if h.NumBytes != nullableBodySize {
		return nil, fmt.Errorf("codec: nullable body size = %d, want %d", h.NumBytes, nullableBodySize)
	}
	body, err := br.Read(nullableBodySize)
	if err != nil {
		return nil, fmt.Errorf("codec: reading nullable null count: %w", err)
	}
	nullCount := binary.LittleEndian.Uint64(body)
	if nullCount == 0 || nullCount > h.Length {
		return nil, fmt.Errorf("codec: nullable null count = %d, want in [1, %d]", nullCount, h.Length)
	}
	values, err := readValues(br, opts)
	if err != nil {
		return nil, fmt.Errorf("codec: reading nullable values: %w", err)
	}
	if values.Length() != h.Length {
		return nil, fmt.Errorf("codec: nullable value length = %d, want %d", values.Length(), h.Length)
	}
	if err := requireNonNullable(values, "nullable values"); err != nil {
		return nil, err
	}
	validity, err := readUnsignedEncodedArray[uint8](br, opts)
	if err != nil {
		return nil, fmt.Errorf("codec: reading nullable validity: %w", err)
	}
	wantBytes := h.Length / 8
	if h.Length&7 != 0 {
		wantBytes++
	}
	if validity.Length() != wantBytes {
		return nil, fmt.Errorf("codec: nullable validity length = %d, want %d", validity.Length(), wantBytes)
	}
	if err := requireNonNullable(validity, "nullable validity"); err != nil {
		return nil, err
	}
	validCount := uint64(0)
	for i := range wantBytes {
		b := validity.ValueAt(i)
		if i+1 == wantBytes && h.Length&7 != 0 {
			mask := uint8(1)<<(h.Length&7) - 1
			if b&^mask != 0 {
				return nil, errors.New("codec: nullable validity has non-zero unused bits")
			}
		}
		validCount += uint64(bits.OnesCount8(b))
	}
	if h.Length-validCount != nullCount {
		return nil, fmt.Errorf("codec: nullable bitmap has %d nulls, header says %d", h.Length-validCount, nullCount)
	}
	return &nullableArray[T]{values: values, validity: validity, nullCount: nullCount}, nil
}
