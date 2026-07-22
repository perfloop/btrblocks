package codec

import (
	"fmt"
	"io"
	"unsafe"

	"github.com/axiomhq/btrblocks/array"
)

// deltaArray stores a base value plus a child encoded array of deltas.
// The child has N-1 elements where child[i] = values[i+1] - values[i].
// Reconstruction is a prefix sum: values[0] = base, values[i] = values[i-1] + child[i-1].
type deltaArray[T Integer] struct {
	encodedNode
	decodeLimit
	base  T
	child EncodedArray[T]
}

func (d *deltaArray[T]) CodecType() CodecType       { return CodecTypeDelta }
func (d *deltaArray[T]) Length() uint64             { return d.child.Length() + 1 }
func (d *deltaArray[T]) IsValid(offset uint64) bool { return allValidAt(d.Length(), offset) }
func (d *deltaArray[T]) NullCount() uint64          { return 0 }
func (d *deltaArray[T]) PType() PType               { return array.PTypeOfPrimitive[T]() }

// DecodedBytes adds one element to the child's: DecompressInto decodes the
// N-1 deltas into dst[1:], so dst is the child's destination plus the base
// slot, and only the child's own intermediates are extra.
func (d *deltaArray[T]) DecodedBytes() (uint64, error) {
	var f decodeFootprint
	f.add(d.child.DecodedBytes())
	f.add(decodedBytesFor(1, d.PType()))
	return f.result()
}

func (d *deltaArray[T]) BinarySize() uint64 {
	return uint64(headerSize) + uint64(unsafe.Sizeof(d.base)) + d.child.BinarySize()
}

func (d *deltaArray[T]) MarshalBinary() ([]byte, error) { return marshalBinary(d) }

func (d *deltaArray[T]) ValueAt(offset uint64) T {
	if offset >= d.Length() {
		panic(errOffsetOutOfRange)
	}
	if offset == 0 {
		return d.base
	}
	value := d.base
	for i := range offset {
		value += d.child.ValueAt(i)
	}
	return value
}

func (d *deltaArray[T]) DecompressInto(dst []T) error {
	n := d.child.Length() + 1
	if err := checkDstLen(dst, n); err != nil {
		return err
	}
	// Decompress deltas into dst[1:], then prefix-sum in-place.
	if err := d.child.DecompressInto(dst[1:]); err != nil {
		return err
	}
	dst[0] = d.base
	for i := uint64(1); i < n; i++ {
		dst[i] += dst[i-1]
	}
	return nil
}

func (d *deltaArray[T]) Slice(start, end uint64) (EncodedArray[T], error) {
	return sliceByDecoding(d, d.decodeLimit, start, end, slicePrimitiveToRawArray[T])
}

func (d *deltaArray[T]) WriteTo(w io.Writer) (int64, error) {
	baseSize := uint64(unsafe.Sizeof(d.base))
	sum := writeSum{w: w}
	if err := sum.writeTo(codecHeader{
		Version:  versionNumber,
		Type:     CodecTypeDelta,
		ElemType: array.PTypeOfPrimitive[T](),
		Length:   d.child.Length() + 1,
		NumBytes: baseSize,
	}); err != nil {
		return sum.n, err
	}
	if err := sum.add(writeIntegerLE(sum.w, d.base)); err != nil {
		return sum.n, err
	}
	if err := sum.writeTo(d.child); err != nil {
		return sum.n, err
	}
	return sum.n, nil
}

func readDeltaArray[T Integer](br *array.BufReader, h codecHeader, opts *readOptions, readValues encodedReader[T]) (EncodedArray[T], error) {
	baseSize := uint64(unsafe.Sizeof(T(0)))
	if h.NumBytes != baseSize {
		return nil, fmt.Errorf("codec: delta body size = %d, want %d", h.NumBytes, baseSize)
	}
	if h.Length == 0 {
		return nil, fmt.Errorf("codec: delta length = 0")
	}

	data, err := br.Read(int(baseSize))
	if err != nil {
		return nil, fmt.Errorf("codec: reading delta base: %w", err)
	}
	base := readIntegerLE[T](data)

	child, err := readValues(br, opts)
	if err != nil {
		return nil, fmt.Errorf("codec: reading delta child: %w", err)
	}
	if err := requireNonNullable(child, "delta"); err != nil {
		return nil, err
	}
	if child.Length()+1 != h.Length {
		return nil, fmt.Errorf("codec: delta child length = %d, want %d", child.Length(), h.Length-1)
	}
	return &deltaArray[T]{decodeLimit: opts.decodeLimit(), base: base, child: child}, nil
}

// encodeDelta transforms arr into residuals, delegates their compression to
// buildChild, and assembles a Delta node. Selection and recursion policy stay
// with the caller.
func encodeDelta[T Integer](arr array.ArrayCore[T], buildChild ChildBuilder[T]) (EncodedArray[T], error) {
	n := arr.Length()
	if n < 2 {
		return nil, errDataEmpty
	}
	if buildChild == nil {
		return nil, ErrBuilderRequired
	}

	base := arr.ValueAt(0)

	// The recursive planner materializes this virtual residual array before
	// retaining it, so it is safe to read directly from arr here. Avoiding a
	// second full-size values copy matters on large Delta candidates.
	child, err := buildChild(array.NewVirtual(n-1, func(i uint64) T { return arr.ValueAt(i+1) - arr.ValueAt(i) }))
	child, err = adoptChild(n-1, child, err)
	if err != nil {
		return nil, fmt.Errorf("codec: compress delta residuals: %w", err)
	}
	return &deltaArray[T]{base: base, child: child}, nil
}
