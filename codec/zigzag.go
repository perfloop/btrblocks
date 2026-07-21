package codec

import (
	"fmt"
	"io"

	"github.com/axiomhq/btrblocks/array"
)

// zigzagArray stores signed integers by delegating to an unsigned child array.
type zigzagArray[T SignedInteger, U UnsignedInteger] struct {
	encodedNode
	decodeLimit
	child EncodedArray[U]
}

func zigzagEncode64(n int64) uint64                     { return uint64((n << 1) ^ (n >> 63)) }
func zigzagDecode64(z uint64) int64                     { return int64(z>>1) ^ -int64(z&1) }
func zigzagEncodeValue[T SignedInteger](value T) uint64 { return zigzagEncode64(int64(value)) }

func zigzagMaxEncoded[T SignedInteger](length uint64, valueAt func(uint64) T) uint64 {
	var maxEncoded uint64
	for i := range length {
		if encoded := zigzagEncodeValue(valueAt(i)); encoded > maxEncoded {
			maxEncoded = encoded
		}
	}
	return maxEncoded
}

func (z *zigzagArray[T, U]) CodecType() CodecType {
	return CodecTypeZigZag
}
func (z *zigzagArray[T, U]) Length() uint64 { return z.child.Length() }
func (z *zigzagArray[T, U]) IsValid(offset uint64) bool {
	return allValidAt(z.Length(), offset)
}
func (z *zigzagArray[T, U]) NullCount() uint64 { return 0 }
func (z *zigzagArray[T, U]) PType() PType      { return array.PTypeOfPrimitive[T]() }
func (z *zigzagArray[T, U]) DecodedBytes() (uint64, error) {
	// DecompressInto decodes the unsigned child into its own buffer and then
	// widens it into dst, so both are live at once.
	var f decodeFootprint
	f.add(decodedBytesFor(z.Length(), z.PType()))
	f.add(z.child.DecodedBytes())
	return f.result()
}
func (z *zigzagArray[T, U]) BinarySize() uint64 { return uint64(headerSize) + z.child.BinarySize() }

func (z *zigzagArray[T, U]) ValueAt(offset uint64) T {
	value := uint64(z.child.ValueAt(offset))
	return T(zigzagDecode64(value))
}

func (z *zigzagArray[T, U]) DecompressInto(dst []T) error {
	if err := checkDstLen(dst, z.child.Length()); err != nil {
		return err
	}
	encoded, err := decompress(z.child, z.maxDecodedBytes())
	if err != nil {
		return err
	}
	for i, value := range encoded {
		dst[i] = T(zigzagDecode64(uint64(value)))
	}
	return nil
}

func (z *zigzagArray[T, U]) Slice(start, end uint64) (EncodedArray[T], error) {
	child, err := z.child.Slice(start, end)
	if err != nil {
		return nil, err
	}
	return &zigzagArray[T, U]{decodeLimit: z.decodeLimit, child: child}, nil
}

func (z *zigzagArray[T, U]) WriteTo(w io.Writer) (int64, error) {
	n, err := codecHeader{
		Version:  versionNumber,
		Type:     CodecTypeZigZag,
		ElemType: array.PTypeOfPrimitive[T](),
		Length:   z.child.Length(),
		NumBytes: 0,
	}.WriteTo(w)
	if err != nil {
		return n, err
	}
	nn, err := z.child.WriteTo(w)
	return n + nn, err
}

func readZigZagArray[T SignedInteger](br *array.BufReader, h codecHeader, opts *readOptions) (EncodedArray[T], error) {
	if h.NumBytes != 0 {
		return nil, fmt.Errorf("codec: zigzag body size = %d, want 0", h.NumBytes)
	}
	childHeader, err := readHeader(br)
	if err != nil {
		return nil, fmt.Errorf("codec: reading zigzag child header: %w", err)
	}
	switch childHeader.ElemType {
	case PTypeUint8:
		return readZigZagChild[T, uint8](br, h, childHeader, opts)
	case PTypeUint16:
		return readZigZagChild[T, uint16](br, h, childHeader, opts)
	case PTypeUint32:
		return readZigZagChild[T, uint32](br, h, childHeader, opts)
	case PTypeUint64:
		return readZigZagChild[T, uint64](br, h, childHeader, opts)
	default:
		return nil, fmt.Errorf("codec: zigzag child type = %v, want unsigned integer", childHeader.ElemType)
	}
}

func readZigZagChild[T SignedInteger, U UnsignedInteger](br *array.BufReader, h codecHeader, childHeader codecHeader, opts *readOptions) (EncodedArray[T], error) {
	child, err := readUnsignedEncodedArrayWithHeader[U](br, childHeader, opts)
	if err != nil {
		return nil, fmt.Errorf("codec: reading zigzag child: %w", err)
	}
	if err := requireNonNullable(child, "zigzag"); err != nil {
		return nil, err
	}
	if child.Length() != h.Length {
		return nil, fmt.Errorf("codec: zigzag length = %d, want %d", child.Length(), h.Length)
	}
	return &zigzagArray[T, U]{decodeLimit: opts.decodeLimit(), child: child}, nil
}

// zigZagEncodedType returns the narrowest unsigned primitive type that can
// hold every ZigZag-transformed value in arr.
func zigZagEncodedType[T SignedInteger](arr array.ArrayCore[T]) PType {
	maxEncoded := zigzagMaxEncoded(arr.Length(), arr.ValueAt)
	switch {
	case maxEncoded <= uint64(^uint8(0)):
		return PTypeUint8
	case maxEncoded <= uint64(^uint16(0)):
		return PTypeUint16
	case maxEncoded <= uint64(^uint32(0)):
		return PTypeUint32
	default:
		return PTypeUint64
	}
}

// encodeZigZagAs transforms arr into unsigned U values, delegates their
// compression to buildChild, and assembles a ZigZag node.
func encodeZigZagAs[T SignedInteger, U UnsignedInteger](arr array.ArrayCore[T], buildChild ChildBuilder[U]) (EncodedArray[T], error) {
	if buildChild == nil {
		return nil, ErrBuilderRequired
	}
	child, err := buildChild(array.NewVirtual(arr.Length(), func(i uint64) U { return U(zigzagEncodeValue(arr.ValueAt(i))) }))
	child, err = adoptChild(arr.Length(), child, err)
	if err != nil {
		return nil, fmt.Errorf("codec: compress zig-zag values: %w", err)
	}
	return &zigzagArray[T, U]{child: child}, nil
}
