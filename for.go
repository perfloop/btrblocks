package btrblocks

import (
	"fmt"
	"io"
	"unsafe"

	"github.com/axiomhq/btrblocks/array"
)

// forArray stores a reference value plus a child encoded array for biased deltas.
type forArray[T Integer] struct {
	min   T
	child EncodedArray[T]
}

func (f *forArray[T]) CodecType() CodecType          { return CodecTypeFor }
func (f *forArray[T]) Length() uint64                { return f.child.Length() }
func (f *forArray[T]) IsValid(offset uint64) bool    { return allValidAt(f.Length(), offset) }
func (f *forArray[T]) NullCount() uint64             { return 0 }
func (f *forArray[T]) PType() PType                  { return array.PTypeOfPrimitive[T]() }
func (f *forArray[T]) DecodedBytes() (uint64, error) { return decodedBytesFor(f.Length(), f.PType()) }

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
	for i := range dst[:f.child.Length()] {
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
	sum := writeSum{w: w}
	if err := sum.writeTo(codecHeader{
		Version:  versionNumber,
		Type:     CodecTypeFor,
		ElemType: array.PTypeOfPrimitive[T](),
		Length:   f.child.Length(),
		NumBytes: minSize,
	}); err != nil {
		return sum.n, err
	}
	if err := sum.add(writeIntegerLE(sum.w, f.min)); err != nil {
		return sum.n, err
	}
	if err := sum.writeTo(f.child); err != nil {
		return sum.n, err
	}
	return sum.n, nil
}

func readFoRArray[T Integer](br *array.BufReader, h codecHeader, opts ReadOptions, readValues encodedReader[T]) (EncodedArray[T], error) {
	minSize := uint64(unsafe.Sizeof(T(0)))
	if h.NumBytes != minSize {
		return nil, fmt.Errorf("codec: for body size = %d, want %d", h.NumBytes, minSize)
	}

	data, err := br.Read(int(minSize))
	if err != nil {
		return nil, fmt.Errorf("codec: for minimum: %w", err)
	}
	minValue := readIntegerLE[T](data)

	child, err := readValues(br, opts)
	if err != nil {
		return nil, fmt.Errorf("codec: for child: %w", err)
	}
	if err := requireNonNullable(child, "for"); err != nil {
		return nil, err
	}
	if child.Length() != h.Length {
		return nil, fmt.Errorf("codec: for length = %d, want %d", child.Length(), h.Length)
	}
	return &forArray[T]{min: minValue, child: child}, nil
}

// encodeFoR builds a frame-of-reference node.
func encodeFoR[T Integer](arr array.ArrayCore[T]) (EncodedArray[T], error) {
	if arr.Length() == 0 {
		return nil, errDataEmpty
	}

	// Materialize once for min/max scan and downstream bitpack encoding.
	n := arr.Length()
	vals := make([]T, n)
	for i := range n {
		vals[i] = arr.ValueAt(i)
	}

	// Single pass: find min and max range width simultaneously.
	minValue := vals[0]
	maxValue := minValue
	for _, v := range vals[1:] {
		if v < minValue {
			minValue = v
		}
		if v > maxValue {
			maxValue = v
		}
	}
	// Subtract in the unsigned representation. This avoids signed overflow for
	// ranges such as [MinInt8, MaxInt8] while preserving the type-width modulo
	// arithmetic used by the biased delta view.
	rangeWidth := bitWidthForUnsigned(uint64(maxValue) - uint64(minValue))

	// Build bitpack child with known width. FoR deltas are always in
	// [0, max-min], so the optimal bitpack width equals rangeWidth with no
	// exceptions. We pass the width hint to skip the histogram pass inside
	// buildBitPackedArray (saves another N-element scan).
	child, err := buildBitPackedArrayWithWidth(array.NewVirtual(n, func(i uint64) T { return vals[i] - minValue }), rangeWidth)
	if err != nil {
		return nil, err
	}
	return &forArray[T]{min: minValue, child: child}, nil
}
