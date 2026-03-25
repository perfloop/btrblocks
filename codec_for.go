package btrblocks

import (
	"fmt"
	"io"
	"unsafe"

	"github.com/axiomhq/btrblocks/array"
)

// forArray stores a reference value plus a child encoded array for biased deltas.
type forArray[T UnsignedInteger] struct {
	min   T
	child EncodedArray[T]
}

// forEncodedArray exposes values shifted by the shared minimum for child encoding.
type forEncodedArray[T UnsignedInteger] struct {
	length  uint64
	min     T
	valueAt func(uint64) T
}

func (a forEncodedArray[T]) ValueAt(offset uint64) T {
	return a.valueAt(offset) - a.min
}

func (a forEncodedArray[T]) CopyTo(dst []T) {
	for i := range dst {
		dst[i] = a.valueAt(uint64(i)) - a.min
	}
}

func (a forEncodedArray[T]) BinarySize() uint64 {
	return array.HeaderSize + a.length*uint64(array.PTypeForType[T]().ByteWidth())
}

func (a forEncodedArray[T]) Length() uint64 { return a.length }
func (a forEncodedArray[T]) PType() PType   { return array.PTypeForType[T]() }

func (a forEncodedArray[T]) Slice(start, end uint64) (array.Array[T], error) {
	return materializeSlice(a, start, end)
}

func (a forEncodedArray[T]) WriteTo(w io.Writer) (int64, error) {
	return writeVirtualArray(w, a.length, func(i uint64) T {
		return a.valueAt(i) - a.min
	})
}

func (f *forArray[T]) Encoding() CodeType { return CodecTypeFor }
func (f *forArray[T]) Length() uint64     { return f.child.Length() }
func (f *forArray[T]) PType() PType       { return array.PTypeForType[T]() }

func (f *forArray[T]) BinarySize() uint64 {
	return uint64(headerSize) + uint64(unsafe.Sizeof(f.min)) + f.child.BinarySize()
}

func (f *forArray[T]) ValueAt(offset uint64) T {
	return f.child.ValueAt(offset) + f.min
}

func (f *forArray[T]) DecompressInto(dst []T) error {
	if err := f.child.DecompressInto(dst); err != nil {
		return err
	}
	for i := uint64(0); i < f.child.Length(); i++ {
		dst[i] += f.min
	}
	return nil
}

func (f *forArray[T]) Slice(start, end uint64) (EncodedArray[T], error) {
	child, err := f.child.Slice(start, end)
	if err != nil {
		return nil, err
	}
	return &forArray[T]{min: f.min, child: child}, nil
}

func (f *forArray[T]) WriteTo(w io.Writer) (int64, error) {
	minSize := uint64(unsafe.Sizeof(f.min))
	n, err := codecHeader{
		Version:  versionNumber,
		Kind:     CodecTypeFor,
		ElemType: array.PTypeForType[T](),
		Length:   f.child.Length(),
		NumBytes: minSize,
	}.WriteTo(w)
	if err != nil {
		return n, err
	}

	nn, err := writeIntegerLE(w, f.min)
	n += nn
	if err != nil {
		return n, err
	}

	nn, err = f.child.WriteTo(w)
	return n + nn, err
}

func readAnyFoRArray[T Integer | Float | String](br *array.BufReader, h codecHeader, opts ReadOptions) (EncodedArray[T], error) {
	var zero T
	switch any(zero).(type) {
	case uint8:
		return readCast[T](readFoRArray[uint8](br, h, opts))
	case uint16:
		return readCast[T](readFoRArray[uint16](br, h, opts))
	case uint32:
		return readCast[T](readFoRArray[uint32](br, h, opts))
	case uint64:
		return readCast[T](readFoRArray[uint64](br, h, opts))
	default:
		return nil, fmt.Errorf("codec: for not supported for %v", h.ElemType)
	}
}

func readFoRArray[T UnsignedInteger](br *array.BufReader, h codecHeader, opts ReadOptions) (EncodedArray[T], error) {
	minSize := uint64(unsafe.Sizeof(T(0)))
	if h.NumBytes != minSize {
		return nil, fmt.Errorf("codec: for body size = %d, want %d", h.NumBytes, minSize)
	}

	data, err := br.Read(int(minSize))
	if err != nil {
		return nil, err
	}
	minValue := readIntegerLE[T](data)

	child, err := readEncodedArray[T](br, opts)
	if err != nil {
		return nil, err
	}
	if child.Length() != h.Length {
		return nil, fmt.Errorf("codec: for length = %d, want %d", child.Length(), h.Length)
	}
	return &forArray[T]{min: minValue, child: child}, nil
}

func buildFoRArray[T UnsignedInteger](arr array.ArrayCore[T], ctx planContext) (EncodedArray[T], error) {
	if ctx.depth <= 0 {
		return nil, errDepthExhausted
	}
	if arr.Length() == 0 {
		return nil, errDataEmpty
	}

	// Single pass: find min and max range width simultaneously. This
	// replaces two separate N-element passes over arr.ValueAt.
	n := arr.Length()
	minValue := arr.ValueAt(0)
	maxValue := minValue
	for i := uint64(1); i < n; i++ {
		v := arr.ValueAt(i)
		if v < minValue {
			minValue = v
		}
		if v > maxValue {
			maxValue = v
		}
	}
	rangeWidth := bitWidthForUnsigned(uint64(maxValue - minValue))

	// Build bitpack child with known width. FoR deltas are always in
	// [0, max-min], so the optimal bitpack width equals rangeWidth with no
	// exceptions. We pass the width hint to skip the histogram pass inside
	// buildBitPackedArray (saves another N-element scan).
	child, err := buildBitPackedArrayWithWidth(forEncodedArray[T]{
		length:  n,
		min:     minValue,
		valueAt: arr.ValueAt,
	}, rangeWidth)
	if err != nil {
		return nil, err
	}
	return &forArray[T]{min: minValue, child: child}, nil
}

func estimateFoR[T UnsignedInteger](ctx planContext, minValue, maxValue T) (float64, bool) {
	if ctx.depth <= 0 || minValue == 0 {
		return 0, false
	}

	bitpackWidth := bitWidthForUnsigned(uint64(maxValue))
	rangeWidth := bitWidthForUnsigned(uint64(maxValue - minValue))
	if rangeWidth == 0 || rangeWidth >= bitpackWidth {
		return 0, false
	}

	// Bit-width ratio (matches Vortex FORScheme). Cheaper than materializing
	// byte sizes and sufficient for scheme ranking. May over-estimate for very
	// small arrays where per-node header overhead dominates, but headers are
	// noise at scale.
	fullWidth := uint(array.PTypeForType[T]().ByteWidth()) * 8
	return float64(fullWidth) / float64(rangeWidth), true
}
