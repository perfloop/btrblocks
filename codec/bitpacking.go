package codec

import (
	"encoding/binary"
	"fmt"
	"io"
	"math/bits"

	"github.com/axiomhq/btrblocks/array"
)

const flagBitpackHasPatches uint32 = 1 << 0

// bitPackedArray stores fixed-width packed values plus optional patches.
type bitPackedArray[T Integer, I UnsignedInteger] struct {
	encodedNode
	denseRows
	bitWidth uint
	buf      []byte
	patches  *patches[T, I]
}

func bitWidthForUnsigned(value uint64) uint {
	return uint(bits.Len64(value))
}

func unsignedBitWidthHistogram[T Integer](arr interface {
	Length() uint64
	ValueAt(uint64) T
}) []uint64 {
	n := arr.Length()
	histogram := make([]uint64, array.PTypeOfPrimitive[T]().ByteWidth()*8+1)
	for i := range n {
		histogram[bitWidthForUnsigned(uint64(arr.ValueAt(i)))]++
	}
	return histogram
}

func bytesPerBitpackException[T Integer]() uint64 {
	return uint64(array.PTypeOfPrimitive[T]().ByteWidth() + 4)
}

func findBestBitpackWidth[T Integer](histogram []uint64) uint {
	length := uint64(0)
	for _, freq := range histogram {
		length += freq
	}

	numPacked := uint64(0)
	bestCost := length * bytesPerBitpackException[T]()
	bestWidth := uint(0)
	for bitWidth, freq := range histogram {
		packedCost := (uint64(bitWidth)*length + 7) / 8
		numPacked += freq
		exceptionsCost := (length - numPacked) * bytesPerBitpackException[T]()
		if cost := packedCost + exceptionsCost; cost < bestCost {
			bestCost = cost
			bestWidth = uint(bitWidth)
		}
	}
	return bestWidth
}

// encodeBitpack builds a bit-packed integer node.
func encodeBitpack[T Integer](arr array.ArrayCore[T], budget buildBudget) (EncodedArray[T], error) {
	bitWidth := findBestBitpackWidth[T](unsignedBitWidthHistogram(arr))
	return buildBitPackedArrayCore(arr, bitWidth, budget)
}

// buildBitPackedArrayWithWidth builds a bitpacked array using a caller-supplied
// bit width, skipping the histogram pass. Used by FoR where the optimal width
// is already known from the min/max range.
func buildBitPackedArrayWithWidth[T Integer](arr array.ArrayCore[T], bitWidth uint, budget buildBudget) (*bitPackedArray[T, uint64], error) {
	return buildBitPackedArrayCore(arr, bitWidth, budget)
}

func buildBitPackedArrayCore[T Integer](arr array.ArrayCore[T], bitWidth uint, budget buildBudget) (*bitPackedArray[T, uint64], error) {
	n := arr.Length()
	codec := &bitPackedArray[T, uint64]{denseRows: denseRows(n), bitWidth: bitWidth}
	if n == 0 {
		return codec, nil
	}

	// Materialize to separate interface dispatch from the encoding loop.
	// Bitpack is the leaf codec for FoR, dict, zigzag, etc., so this
	// benefits nearly every compress path.
	vals, err := makeBuildSlice[T](budget, n, n, "bitpack source values")
	if err != nil {
		return nil, err
	}
	for i := range n {
		vals[i] = arr.ValueAt(i)
	}

	if bitWidth > 0 {
		packedSize, err := checkedPackedByteSize(n, bitWidth)
		if err != nil {
			return nil, err
		}
		codec.buf, err = makeBuildSlice[byte](budget, uint64(packedSize), uint64(packedSize), "bitpack payload")
		if err != nil {
			return nil, err
		}
	}
	var patchIdx []uint64
	var patchVals []T
	for i, value := range vals {
		v := uint64(value)
		if bitWidth > 0 {
			packUnsigned(codec.buf, uint64(i)*uint64(bitWidth), bitWidth, v)
		}
		if bitWidthForUnsigned(v) > bitWidth {
			patchIdx, err = appendBuildValue(patchIdx, uint64(i), budget, "bitpack patch indices")
			if err != nil {
				return nil, err
			}
			patchVals, err = appendBuildValue(patchVals, value, budget, "bitpack patch values")
			if err != nil {
				return nil, err
			}
		}
	}
	if len(patchIdx) == 0 {
		return codec, nil
	}

	patchIdxCodec := newRawArray(array.NewPrimitivesUnsafe(patchIdx))
	patchValCodec, err := buildBitpackPatchValues(patchVals)
	if err != nil {
		return nil, err
	}
	p, err := newPatches(n, 0, patchIdxCodec, patchValCodec, patchIdx)
	if err != nil {
		return nil, err
	}
	codec.patches = p
	return codec, nil
}

func buildBitpackPatchValues[T Integer](values []T) (EncodedArray[T], error) {
	arr := array.NewPrimitivesUnsafe(values)
	if isAllSameUnsigned(values) {
		return newConstIntegerArray(arr)
	}
	return newRawArray(arr), nil
}

func isAllSameUnsigned[T Integer](values []T) bool {
	if len(values) == 0 {
		return true
	}
	first := values[0]
	for _, value := range values[1:] {
		if value != first {
			return false
		}
	}
	return true
}

func (c *bitPackedArray[T, I]) CodecType() CodecType {
	return CodecTypeBitpack
}
func (c *bitPackedArray[T, I]) PType() PType { return array.PTypeOfPrimitive[T]() }
func (c *bitPackedArray[T, I]) DecodedBytes() (uint64, error) {
	// Unpacking writes straight into dst; only the patch children add buffers.
	var f decodeFootprint
	f.add(decodedBytesFor(c.Length(), c.PType()))
	f.add(c.patches.decodedBytes())
	return f.result()
}
func (c *bitPackedArray[T, I]) BinarySize() uint64 {
	size := uint64(headerSize) + 1 + uint64(len(c.buf))
	if c.patches != nil {
		size += c.patches.BinarySize()
	}
	return size
}

func (c *bitPackedArray[T, I]) ValueAt(offset uint64) T {
	if offset >= c.Length() {
		panic(errOffsetOutOfRange)
	}
	if value, ok := c.patches.ValueAt(offset); ok {
		return value
	}
	if c.bitWidth == 0 {
		var zero T
		return zero
	}
	return T(unpackUnsigned(c.buf, offset*uint64(c.bitWidth), c.bitWidth))
}

func (c *bitPackedArray[T, I]) DecompressInto(dst []T) error {
	if err := checkDstLen(dst, c.Length()); err != nil {
		return err
	}
	if c.bitWidth == 0 {
		clear(dst[:c.Length()])
	} else {
		unpackBatchTyped(c.buf, c.Length(), c.bitWidth, dst[:c.Length()])
	}
	return c.patches.Apply(dst[:c.Length()])
}

// unpackBatchTyped decodes count packed values directly into a typed slice.
func unpackBatchTyped[T Integer](buf []byte, count uint64, bitWidth uint, dst []T) {
	mask := uint64((1 << bitWidth) - 1)
	bitOff := uint64(0)
	bufLen := uint64(len(buf))
	for i := range count {
		byteOff := bitOff >> 3
		bitInByte := uint(bitOff & 7)
		if bitWidth+bitInByte <= 64 && byteOff+8 <= bufLen {
			word := binary.LittleEndian.Uint64(buf[byteOff:])
			dst[i] = T((word >> bitInByte) & mask)
		} else {
			dst[i] = T(unpackUnsigned(buf, bitOff, bitWidth))
		}
		bitOff += uint64(bitWidth)
	}
}

func (c *bitPackedArray[T, I]) Slice(start, end uint64) (EncodedArray[T], error) {
	if err := array.ValidateSliceBounds(c.Length(), start, end); err != nil {
		return nil, err
	}
	length := end - start
	sliced := &bitPackedArray[T, I]{denseRows: denseRows(length), bitWidth: c.bitWidth}
	if c.bitWidth > 0 && length > 0 {
		sliced.buf = make([]byte, packedByteSize(length, c.bitWidth))
		for i := range length {
			value := unpackUnsigned(c.buf, (start+i)*uint64(c.bitWidth), c.bitWidth)
			packUnsigned(sliced.buf, i*uint64(c.bitWidth), c.bitWidth, value)
		}
	}
	patches, err := c.patches.Slice(start, end)
	if err != nil {
		return nil, err
	}
	sliced.patches = patches
	return sliced, nil
}

func (c *bitPackedArray[T, I]) WriteTo(w io.Writer) (int64, error) {
	flags := uint32(0)
	if c.patches != nil {
		flags |= flagBitpackHasPatches
	}
	sum := writeSum{w: w}
	if err := sum.writeTo(codecHeader{
		Version:  versionNumber,
		Type:     CodecTypeBitpack,
		ElemType: array.PTypeOfPrimitive[T](),
		Flags:    flags,
		Length:   c.Length(),
		NumBytes: 1 + uint64(len(c.buf)),
	}); err != nil {
		return sum.n, err
	}

	var width [1]byte
	width[0] = byte(c.bitWidth)
	if err := sum.write(width[:]); err != nil {
		return sum.n, err
	}

	if err := sum.write(c.buf); err != nil {
		return sum.n, err
	}
	if c.patches != nil {
		if err := sum.writeTo(c.patches); err != nil {
			return sum.n, err
		}
	}
	return sum.n, nil
}

func readBitPackedArray[T Integer](br *array.BufReader, h codecHeader, opts *readOptions, readValues encodedReader[T]) (EncodedArray[T], error) {
	if h.Flags&^flagBitpackHasPatches != 0 {
		return nil, fmt.Errorf("codec: unsupported bitpack flags = 0x%x", h.Flags)
	}
	if h.NumBytes < 1 {
		return nil, fmt.Errorf("codec: bitpack body too small")
	}

	data, err := br.Read(1)
	if err != nil {
		return nil, fmt.Errorf("codec: bitpack bit width: %w", err)
	}
	bitWidth := uint(data[0])
	maxBitWidth := uint(array.PTypeOfPrimitive[T]().ByteWidth() * 8)
	if bitWidth > maxBitWidth {
		return nil, fmt.Errorf("codec: bit width = %d exceeds %T width", bitWidth, *new(T))
	}

	bufSize, err := checkedPackedByteSize(h.Length, bitWidth)
	if err != nil {
		return nil, err
	}
	if h.NumBytes != 1+uint64(bufSize) {
		return nil, fmt.Errorf("codec: bitpack body size = %d, want %d", h.NumBytes, 1+uint64(bufSize))
	}

	var buf []byte
	if bufSize > 0 {
		buf, err = br.Read(bufSize)
		if err != nil {
			return nil, fmt.Errorf("codec: bitpack packed values: %w", err)
		}
	}

	if h.Flags&flagBitpackHasPatches == 0 {
		return &bitPackedArray[T, uint64]{denseRows: denseRows(h.Length), bitWidth: bitWidth, buf: buf}, nil
	}

	data, err = br.Read(8)
	if err != nil {
		return nil, fmt.Errorf("codec: bitpack patch offset: %w", err)
	}
	offset := binary.LittleEndian.Uint64(data)

	idxHeader, err := readHeader(br)
	if err != nil {
		return nil, fmt.Errorf("codec: bitpack patch indices header: %w", err)
	}
	switch idxHeader.ElemType {
	case PTypeUint8:
		return readBitPackedWithPatchIdx[T, uint8](br, h, opts, bitWidth, buf, offset, idxHeader, readValues)
	case PTypeUint16:
		return readBitPackedWithPatchIdx[T, uint16](br, h, opts, bitWidth, buf, offset, idxHeader, readValues)
	case PTypeUint32:
		return readBitPackedWithPatchIdx[T, uint32](br, h, opts, bitWidth, buf, offset, idxHeader, readValues)
	case PTypeUint64:
		return readBitPackedWithPatchIdx[T, uint64](br, h, opts, bitWidth, buf, offset, idxHeader, readValues)
	default:
		return nil, fmt.Errorf("codec: bitpack patch index type = %v, want unsigned integer", idxHeader.ElemType)
	}
}

func readBitPackedWithPatchIdx[T Integer, I UnsignedInteger](br *array.BufReader, h codecHeader, opts *readOptions, bitWidth uint, buf []byte, offset uint64, idxHeader codecHeader, readValues encodedReader[T]) (EncodedArray[T], error) {
	idxCodec, err := readUnsignedEncodedArrayWithHeader[I](br, idxHeader, opts)
	if err != nil {
		return nil, fmt.Errorf("codec: reading bitpack patch indices: %w", err)
	}
	if err := requireNonNullable(idxCodec, "bitpack patch indices"); err != nil {
		return nil, err
	}
	valCodec, err := readValues(br, opts)
	if err != nil {
		return nil, fmt.Errorf("codec: reading bitpack patch values: %w", err)
	}
	if err := requireNonNullable(valCodec, "bitpack patch values"); err != nil {
		return nil, err
	}
	p, err := readPatches(h.Length, offset, idxCodec, valCodec, opts)
	if err != nil {
		return nil, prefixPatchError(err, "bitpack")
	}
	return &bitPackedArray[T, I]{denseRows: denseRows(h.Length), bitWidth: bitWidth, buf: buf, patches: p}, nil
}

func packedByteSize(length uint64, bitWidth uint) int {
	size, err := checkedPackedByteSize(length, bitWidth)
	if err != nil {
		panic(err)
	}
	return size
}

func checkedPackedByteSize(length uint64, bitWidth uint) (int, error) {
	if length == 0 || bitWidth == 0 {
		return 0, nil
	}
	maxInt := uint64(^uint(0) >> 1)
	if length > (^uint64(0)-7)/uint64(bitWidth) {
		return 0, fmt.Errorf("codec: payload size overflows for length %d and bit width %d", length, bitWidth)
	}
	size := (length*uint64(bitWidth) + 7) / 8
	if size > maxInt {
		return 0, fmt.Errorf("codec: payload size %d exceeds maximum", size)
	}
	return int(size), nil
}

func packUnsigned(buf []byte, bitOffset uint64, bitWidth uint, value uint64) {
	byteOff := bitOffset / 8
	bitInByte := uint(bitOffset % 8)
	if bitWidth+bitInByte <= 64 && byteOff+8 <= uint64(len(buf)) {
		word := binary.LittleEndian.Uint64(buf[byteOff:])
		word |= (value & ((1 << bitWidth) - 1)) << bitInByte
		binary.LittleEndian.PutUint64(buf[byteOff:], word)
		return
	}
	for written := uint(0); written < bitWidth; {
		off := int(bitOffset / 8)
		inByte := uint(bitOffset % 8)
		take := minUint(bitWidth-written, 8-inByte)
		chunk := byte((value >> written) & uint64(maskForBits(take)))
		buf[off] |= chunk << inByte
		written += take
		bitOffset += uint64(take)
	}
}

func unpackUnsigned(buf []byte, bitOffset uint64, bitWidth uint) uint64 {
	byteOff := bitOffset / 8
	bitInByte := uint(bitOffset % 8)
	if bitWidth+bitInByte <= 64 && byteOff+8 <= uint64(len(buf)) {
		word := binary.LittleEndian.Uint64(buf[byteOff:])
		return (word >> bitInByte) & ((1 << bitWidth) - 1)
	}
	var value uint64
	bo := bitOffset
	for read := uint(0); read < bitWidth; {
		off := int(bo / 8)
		inByte := uint(bo % 8)
		take := minUint(bitWidth-read, 8-inByte)
		chunk := (buf[off] >> inByte) & maskForBits(take)
		value |= uint64(chunk) << read
		read += take
		bo += uint64(take)
	}
	return value
}

func maskForBits(width uint) byte {
	if width >= 8 {
		return 0xff
	}
	return byte((uint16(1) << width) - 1)
}

func minUint(a, b uint) uint {
	if a < b {
		return a
	}
	return b
}
