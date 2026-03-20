package btrblocks

import (
	"fmt"

	"github.com/axiomhq/btrblocks/array"
)

type canonicalArray[T Integer | Float | String] struct {
	values    array.Array[T]
	validity  []bool
	nullCount uint64
}

type CanonicalCodec[T Integer | Float | String] interface {
	Length() uint64
	PType() PType
	NullCount() uint64
	Decode(values []T, valid []bool) error
}

type denseCanonicalCodec[T Integer | Float | String] struct {
	codec Codec[T]
}

type nullableCanonicalCodec[T Integer | Float | String] struct {
	length    uint64
	pType     PType
	nullCount uint64
	validity  Codec[uint8]
	values    Codec[T]
}

func newCanonicalArray[T Integer | Float | String](values array.Array[T], validity []bool) (canonicalArray[T], error) {
	if values == nil {
		return canonicalArray[T]{}, fmt.Errorf("canonical: values array is nil")
	}
	if len(validity) == 0 {
		return canonicalArray[T]{values: values}, nil
	}
	if len(validity) != int(values.Length()) {
		return canonicalArray[T]{}, fmt.Errorf("canonical: validity length = %d, want %d", len(validity), values.Length())
	}

	mask := make([]bool, len(validity))
	copy(mask, validity)

	nullCount := uint64(0)
	for _, isValid := range mask {
		if !isValid {
			nullCount++
		}
	}
	if nullCount == 0 {
		mask = nil
	}

	return canonicalArray[T]{
		values:    values,
		validity:  mask,
		nullCount: nullCount,
	}, nil
}

func newDenseCanonicalArray[T Integer | Float | String](values array.Array[T]) canonicalArray[T] {
	return canonicalArray[T]{values: values}
}

func (c canonicalArray[T]) Length() uint64 {
	return c.values.Length()
}

func (c canonicalArray[T]) PType() PType {
	return c.values.PType()
}

func (c canonicalArray[T]) NullCount() uint64 {
	return c.nullCount
}

func (c canonicalArray[T]) IsValid(offset uint64) bool {
	if len(c.validity) == 0 {
		return true
	}
	return c.validity[offset]
}

func (c canonicalArray[T]) NonNullValues() array.Array[T] {
	if c.nullCount == 0 {
		return c.values
	}
	values := make([]T, 0, int(c.Length()-c.nullCount))
	for i := uint64(0); i < c.Length(); i++ {
		if c.IsValid(i) {
			values = append(values, c.values.ValueAt(i))
		}
	}
	return buildArray(values)
}

func (c canonicalArray[T]) validityValues() array.Array[uint8] {
	values := make([]uint8, int(c.Length()))
	for i := uint64(0); i < c.Length(); i++ {
		if c.IsValid(i) {
			values[i] = 1
		}
	}
	return array.NewPrimitivesUnsafe(values)
}

func (c *denseCanonicalCodec[T]) Length() uint64 {
	return c.codec.Length()
}

func (c *denseCanonicalCodec[T]) PType() PType {
	return c.codec.PType()
}

func (c *denseCanonicalCodec[T]) NullCount() uint64 {
	return 0
}

func (c *denseCanonicalCodec[T]) Decode(values []T, valid []bool) error {
	if err := validateDecodeLength(c.codec.Length(), len(values)); err != nil {
		return err
	}
	if len(valid) != 0 && len(valid) != len(values) {
		return fmt.Errorf("canonical: validity destination length = %d, want %d", len(valid), c.codec.Length())
	}
	if err := c.codec.Decode(values); err != nil {
		return err
	}
	if len(valid) > 0 {
		for i := range valid {
			valid[i] = true
		}
	}
	return nil
}

func (c *nullableCanonicalCodec[T]) Length() uint64 {
	return c.length
}

func (c *nullableCanonicalCodec[T]) PType() PType {
	return c.pType
}

func (c *nullableCanonicalCodec[T]) NullCount() uint64 {
	return c.nullCount
}

func (c *nullableCanonicalCodec[T]) Decode(values []T, valid []bool) error {
	if err := validateDecodeLength(c.length, len(values)); err != nil {
		return err
	}
	if len(valid) != len(values) {
		return fmt.Errorf("canonical: validity destination length = %d, want %d", len(valid), c.length)
	}

	mask := make([]uint8, int(c.length))
	if err := c.validity.Decode(mask); err != nil {
		return err
	}

	valueCount := int(c.length - c.nullCount)
	decodedValues := make([]T, valueCount)
	if valueCount > 0 {
		if c.values == nil {
			return fmt.Errorf("canonical: missing values codec")
		}
		if err := c.values.Decode(decodedValues); err != nil {
			return err
		}
	}

	next := 0
	var zero T
	for i, flag := range mask {
		isValid := flag != 0
		valid[i] = isValid
		if !isValid {
			values[i] = zero
			continue
		}
		if next >= len(decodedValues) {
			return fmt.Errorf("canonical: validity/value count mismatch")
		}
		values[i] = decodedValues[next]
		next++
	}
	if next != len(decodedValues) {
		return fmt.Errorf("canonical: unused decoded values = %d", len(decodedValues)-next)
	}
	return nil
}

func CompressCanonical[T Integer | Float | String](values []T, validity []bool, opts Options) (CanonicalCodec[T], error) {
	if values == nil && len(validity) > 0 {
		values = make([]T, len(validity))
	}
	canonical, err := newCanonicalArray(buildArray(values), validity)
	if err != nil {
		return nil, err
	}
	return compressCanonicalArray(canonical, newPlanContext(opts))
}

func DecompressCanonical[T Integer | Float | String](codec CanonicalCodec[T]) ([]T, []bool, error) {
	values := make([]T, int(codec.Length()))
	valid := make([]bool, int(codec.Length()))
	if err := codec.Decode(values, valid); err != nil {
		return nil, nil, err
	}
	return values, valid, nil
}

func compressDenseArray[T Integer | Float | String](arr array.Array[T], ctx planContext) (Codec[T], error) {
	codec, err := compressCanonicalArray(newDenseCanonicalArray(arr), ctx)
	if err != nil {
		return nil, err
	}
	return unwrapDenseCanonicalCodec(codec)
}

func compressCanonicalArray[T Integer | Float | String](arr canonicalArray[T], ctx planContext) (CanonicalCodec[T], error) {
	if arr.NullCount() == 0 {
		codec, err := compressArray(arr.values, ctx)
		if err != nil {
			return nil, err
		}
		return &denseCanonicalCodec[T]{codec: codec}, nil
	}

	childCtx := ctx.descend()
	validityCodec, err := compressDenseArray(arr.validityValues(), childCtx)
	if err != nil {
		return nil, err
	}

	var valuesCodec Codec[T]
	nonNull := arr.NonNullValues()
	if nonNull.Length() > 0 {
		valuesCodec, err = compressDenseArray(nonNull, childCtx)
		if err != nil {
			return nil, err
		}
	}

	return &nullableCanonicalCodec[T]{
		length:    arr.Length(),
		pType:     arr.PType(),
		nullCount: arr.NullCount(),
		validity:  validityCodec,
		values:    valuesCodec,
	}, nil
}

func unwrapDenseCanonicalCodec[T Integer | Float | String](codec CanonicalCodec[T]) (Codec[T], error) {
	dense, ok := codec.(*denseCanonicalCodec[T])
	if !ok {
		return nil, fmt.Errorf("canonical: expected dense codec")
	}
	return dense.codec, nil
}
