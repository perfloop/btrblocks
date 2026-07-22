package compress

import (
	"errors"
	"fmt"
	"io"
	"math"
	"unsafe"

	"github.com/axiomhq/btrblocks/array"
	"github.com/axiomhq/btrblocks/codec"
)

// errOffsetOutOfRange is the panic value the planner's array views raise for an
// out-of-range index, matching array.ArrayCore's documented contract.
var errOffsetOutOfRange = errors.New("offset out of range")

func allValidAt(length, offset uint64) bool {
	if offset >= length {
		panic(errOffsetOutOfRange)
	}
	return true
}

type maskedArray[T array.Integer | array.Float | array.String] struct {
	source      array.Array[T]
	validity    *byte // read-only bitmap borrowed only from a native primitive array
	size        func() uint64
	materialize func([]T) (array.Array[T], error)
	checkExtra  func(uint64) error
}

func checkMaskedAllocation[T array.Integer | array.Float | array.String](length, maxBytes uint64, name string) error {
	var value T
	width := uint64(unsafe.Sizeof(value))
	if width != 0 && length > maxBytes/width {
		return fmt.Errorf("%w: %s needs %d values of width %d, limit %d bytes", codec.ErrMaterializationLimit, name, length, width, maxBytes)
	}
	if length > uint64(^uint(0)>>1) {
		return fmt.Errorf("%w: %s length %d overflows int", codec.ErrMaterializationLimit, name, length)
	}
	return nil
}

func (a *maskedArray[T]) ValueAt(offset uint64) T {
	if a.validity != nil {
		// ValueAt provides the required bounds check before the bitmap pointer is
		// dereferenced. ArrayCore permits reading a null slot's physical value.
		value := a.source.ValueAt(offset)
		if *(*byte)(unsafe.Add(unsafe.Pointer(a.validity), uintptr(offset>>3)))&(1<<(offset&7)) == 0 {
			var zero T
			return zero
		}
		return value
	}
	if !a.source.IsValid(offset) {
		var zero T
		return zero
	}
	return a.source.ValueAt(offset)
}

func (a *maskedArray[T]) Length() uint64 { return a.source.Length() }
func (a *maskedArray[T]) IsValid(offset uint64) bool {
	return allValidAt(a.Length(), offset)
}
func (a *maskedArray[T]) NullCount() uint64  { return 0 }
func (a *maskedArray[T]) PType() array.PType { return a.source.PType() }

// BinarySize delegates caching to the family-specific size closure so the
// native validity bitmap can reuse this view's fixed storage.
func (a *maskedArray[T]) BinarySize() uint64 { return a.size() }

func (a *maskedArray[T]) CopyTo(dst []T) {
	for i := range min(uint64(len(dst)), a.Length()) {
		dst[i] = a.ValueAt(i)
	}
}

func (a *maskedArray[T]) Slice(start, end uint64) (array.Array[T], error) {
	if err := array.ValidateSliceBounds(a.Length(), start, end); err != nil {
		return nil, err
	}
	if err := checkMaskedAllocation[T](end-start, codec.DefaultMaxBuildBytes, "masked slice"); err != nil {
		return nil, err
	}
	values := make([]T, end-start)
	for i := range values {
		values[i] = a.ValueAt(start + uint64(i))
	}
	return a.materialize(values)
}

func (a *maskedArray[T]) WriteTo(w io.Writer) (int64, error) {
	materialized, err := a.materialized()
	if err != nil {
		return 0, err
	}
	return materialized.WriteTo(w)
}

func (a *maskedArray[T]) materialized(limits ...uint64) (array.Array[T], error) {
	maxBytes := uint64(codec.DefaultMaxBuildBytes)
	if len(limits) != 0 && limits[0] != 0 {
		maxBytes = limits[0]
	}
	if err := checkMaskedAllocation[T](a.Length(), maxBytes, "masked values"); err != nil {
		return nil, err
	}
	if a.checkExtra != nil {
		if err := a.checkExtra(maxBytes); err != nil {
			return nil, err
		}
	}
	values := make([]T, a.Length())
	a.CopyTo(values)
	return a.materialize(values)
}

func maskPrimitiveArray[T array.PrimitiveType](source array.Array[T]) *maskedArray[T] {
	masked := &maskedArray[T]{
		source: source,
		size: func() uint64 {
			return array.HeaderSize + source.Length()*uint64(source.PType().ByteWidth())
		},
		materialize: func(values []T) (array.Array[T], error) {
			return array.NewPrimitivesUnsafe(values), nil
		},
	}
	if bitmap := array.PrimitiveValidityBytes(source); len(bitmap) != 0 {
		masked.validity = &bitmap[0]
	}
	return masked
}

func maskStringArray(source array.Array[string]) *maskedArray[string] {
	masked := &maskedArray[string]{
		source: source,
		materialize: func(values []string) (array.Array[string], error) {
			return array.NewStrings(values)
		},
	}
	masked.checkExtra = func(maxBytes uint64) error {
		var total uint64
		for i := range masked.Length() {
			size := uint64(len(masked.ValueAt(i)))
			if size > maxBytes-total {
				return fmt.Errorf("%w: masked string payload exceeds %d bytes", codec.ErrMaterializationLimit, maxBytes)
			}
			total += size
		}
		if masked.Length() == math.MaxUint64 {
			return fmt.Errorf("%w: masked string offsets length overflows", codec.ErrMaterializationLimit)
		}
		offsetWidth := uint64(4)
		switch {
		case total <= math.MaxUint8:
			offsetWidth = 1
		case total <= math.MaxUint16:
			offsetWidth = 2
		}
		if masked.Length()+1 > maxBytes/offsetWidth {
			return fmt.Errorf("%w: masked string offsets exceed %d bytes", codec.ErrMaterializationLimit, maxBytes)
		}
		return nil
	}
	// The masked view's raw size counts null slots as empty payloads, which is
	// what the raw-fallback comparison against candidate encodings needs.
	var binarySize uint64
	masked.size = func() uint64 {
		if binarySize == 0 {
			binarySize = computeStringRawBinarySize(masked)
		}
		return binarySize
	}
	return masked
}

func useNullSparse(length, nullCount uint64, ctx planContext, sparseExcluded bool) bool {
	return nullCount != length && !sparseExcluded && ctx.depth > 0 && countDominates(length, nullCount)
}
