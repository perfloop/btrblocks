package codec

import (
	"fmt"
	"io"

	"github.com/axiomhq/btrblocks/array"
)

var errValueNotConstant = ErrValueNotConstant

// constArray stores one repeated value for the entire logical array length.
type constArray[T Integer | Float | String] struct {
	denseRows
	body array.Array[T]
}

func (c *constArray[T]) CodecType() CodecType          { return CodecTypeConst }
func (c *constArray[T]) PType() PType                  { return c.body.PType() }
func (c *constArray[T]) BinarySize() uint64            { return uint64(headerSize) + c.body.BinarySize() }
func (c *constArray[T]) DecodedBytes() (uint64, error) { return decodedBytesFor(c.Length(), c.PType()) }

func (c *constArray[T]) ValueAt(offset uint64) T {
	if offset >= c.Length() {
		panic(errOffsetOutOfRange)
	}
	return c.body.ValueAt(0)
}

func (c *constArray[T]) DecompressInto(dst []T) error {
	if err := checkDstLen(dst, c.Length()); err != nil {
		return err
	}
	fillRun(dst, 0, int(c.Length()), c.body.ValueAt(0))
	return nil
}

func (c *constArray[T]) Slice(start, end uint64) (EncodedArray[T], error) {
	if err := array.ValidateSliceBounds(c.Length(), start, end); err != nil {
		return nil, err
	}
	if start == end {
		empty, err := c.body.Slice(0, 0)
		if err != nil {
			return nil, err
		}
		return newRawArray(empty), nil
	}
	return &constArray[T]{denseRows: denseRows(end - start), body: c.body}, nil
}

func (c *constArray[T]) WriteTo(w io.Writer) (int64, error) {
	bodySize := c.body.BinarySize()
	n, err := codecHeader{
		Version:  versionNumber,
		Type:     CodecTypeConst,
		ElemType: c.body.PType(),
		Length:   c.Length(),
		NumBytes: bodySize,
	}.WriteTo(w)
	if err != nil {
		return n, err
	}
	nn, err := c.body.WriteTo(w)
	return n + nn, err
}

type constBodyBuilder[T Integer | Float | String] func(T) (array.Array[T], error)

func newConstArray[T Integer | Float | String](arr array.ArrayCore[T], cmp cmpFn[T], buildBody constBodyBuilder[T]) (*constArray[T], error) {
	n := arr.Length()
	if n == 0 {
		return nil, errDataEmpty
	}
	if arr.NullCount() != 0 {
		return nil, errValueNotConstant
	}
	value := arr.ValueAt(0)
	for i := uint64(1); i < n; i++ {
		if !cmp(value, arr.ValueAt(i)) {
			return nil, errValueNotConstant
		}
	}
	body, err := buildBody(value)
	if err != nil {
		return nil, fmt.Errorf("codec: build constant body: %w", err)
	}
	return &constArray[T]{denseRows: denseRows(n), body: body}, nil
}

func newConstIntegerArray[T Integer](arr array.ArrayCore[T]) (*constArray[T], error) {
	return newConstArray(arr, array.CmpIntegers[T], func(value T) (array.Array[T], error) {
		return array.NewPrimitivesUnsafe([]T{value}), nil
	})
}

// encodeConstInteger builds a constant integer node.
func encodeConstInteger[T Integer](arr array.ArrayCore[T]) (EncodedArray[T], error) {
	return newConstIntegerArray(arr)
}

func newConstFloatArray[T Float](arr array.ArrayCore[T]) (*constArray[T], error) {
	return newConstArray(arr, array.CmpFloatBits[T], func(value T) (array.Array[T], error) {
		return array.NewPrimitivesUnsafe([]T{value}), nil
	})
}

// encodeConstFloat builds a constant floating-point node.
func encodeConstFloat[T Float](arr array.ArrayCore[T]) (EncodedArray[T], error) {
	return newConstFloatArray(arr)
}

func newConstStringArray(arr array.ArrayCore[string]) (*constArray[string], error) {
	return newConstArray(arr, array.CmpStrings[string], func(value string) (array.Array[string], error) {
		return array.NewStrings([]string{value})
	})
}

// encodeConstString builds a constant string node.
func encodeConstString(arr array.ArrayCore[string]) (EncodedArray[string], error) {
	return newConstStringArray(arr)
}

type arrayBodyReader[T Integer | Float | String] func(*array.BufReader, ...ReadOptions) (array.Array[T], error)

func readConstArray[T Integer | Float | String](br *array.BufReader, h codecHeader, opts ReadOptions, readBody arrayBodyReader[T]) (EncodedArray[T], error) {
	arr, err := readBody(br, opts)
	if err != nil {
		return nil, fmt.Errorf("codec: const body: %w", err)
	}
	if h.Length == 0 {
		return nil, fmt.Errorf("codec: const length = 0")
	}
	if arr.Length() != 1 {
		return nil, fmt.Errorf("codec: const body length = %d, want 1", arr.Length())
	}
	if err := requireNonNullable(arr, "const body"); err != nil {
		return nil, err
	}
	if h.NumBytes != arr.BinarySize() {
		return nil, fmt.Errorf("codec: const body size = %d, want %d", h.NumBytes, arr.BinarySize())
	}
	return &constArray[T]{denseRows: denseRows(h.Length), body: arr}, nil
}
