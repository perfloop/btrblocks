package btrblocks

import (
	"encoding/binary"
	"fmt"
	"io"
	"math/bits"

	"github.com/axiomhq/btrblocks/array"
)

// bitPackedArray stores fixed-width packed unsigned values plus optional patches.
type bitPackedArray[T UnsignedInteger] struct {
	length   uint64
	bitWidth uint
	buf      []byte
	patches  *patches[T]
}

func bitWidthForUnsigned(value uint64) uint {
	return uint(bits.Len64(value))
}

func bitpackEncodedSize(length uint64, bitWidth uint) (uint64, bool) {
	bodySize, err := checkedPackedByteSize(length, bitWidth)
	if err != nil {
		return 0, false
	}
	return uint64(headerSize) + 1 + uint64(bodySize), true
}

func newBitPackedArrayAtWidth[T UnsignedInteger](arr interface {
	Length() uint64
	ValueAt(uint64) T
}, bitWidth uint) *bitPackedArray[T] {
	codec := &bitPackedArray[T]{length: arr.Length(), bitWidth: bitWidth}
	if arr.Length() == 0 {
		return codec
	}
	if codec.bitWidth == 0 {
		return codec
	}

	codec.buf = make([]byte, packedByteSize(codec.length, codec.bitWidth))
	for i := uint64(0); i < arr.Length(); i++ {
		packUnsigned(codec.buf, i*uint64(codec.bitWidth), codec.bitWidth, uint64(arr.ValueAt(i)))
	}
	return codec
}

func newBitPackedArray[T UnsignedInteger](arr interface {
	Length() uint64
	ValueAt(uint64) T
}) *bitPackedArray[T] {
	var max uint64
	for i := uint64(0); i < arr.Length(); i++ {
		if value := uint64(arr.ValueAt(i)); value > max {
			max = value
		}
	}
	return newBitPackedArrayAtWidth(arr, bitWidthForUnsigned(max))
}

func unsignedBitWidthHistogram[T UnsignedInteger](arr interface {
	Length() uint64
	ValueAt(uint64) T
}) []uint64 {
	histogram := make([]uint64, pTypeForType[T]().ByteWidth()*8+1)
	for i := uint64(0); i < arr.Length(); i++ {
		histogram[bitWidthForUnsigned(uint64(arr.ValueAt(i)))]++
	}
	return histogram
}

func bytesPerBitpackException[T UnsignedInteger]() uint64 {
	return uint64(pTypeForType[T]().ByteWidth() + 4)
}

func findBestBitpackWidth[T UnsignedInteger](histogram []uint64) uint {
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

func buildBitPackedArray[T UnsignedInteger](arr array.Array[T], _ planContext) (EncodedArray[T], error) {
	bitWidth := findBestBitpackWidth[T](unsignedBitWidthHistogram(arr))
	codec := newBitPackedArrayAtWidth(arr, bitWidth)

	patchCount := uint64(0)
	for i := uint64(0); i < arr.Length(); i++ {
		if bitWidthForUnsigned(uint64(arr.ValueAt(i))) > bitWidth {
			patchCount++
		}
	}
	if patchCount == 0 {
		return codec, nil
	}

	patchIdx := make([]uint64, 0, patchCount)
	patchVals := make([]T, 0, patchCount)
	for i := uint64(0); i < arr.Length(); i++ {
		value := arr.ValueAt(i)
		if bitWidthForUnsigned(uint64(value)) <= bitWidth {
			continue
		}
		patchIdx = append(patchIdx, i)
		patchVals = append(patchVals, value)
	}

	patchIdxCodec, err := buildBitpackPatchIndices(patchIdx)
	if err != nil {
		return nil, err
	}
	patchValCodec, err := buildBitpackPatchValues(patchVals)
	if err != nil {
		return nil, err
	}
	patches, err := newPatches(arr.Length(), 0, patchIdxCodec, patchValCodec)
	if err != nil {
		return nil, err
	}
	codec.patches = patches
	return codec, nil
}

func buildBitpackPatchIndices(indices []uint64) (ordinalArray, error) {
	last := indices[len(indices)-1]
	return buildOrdinalSlice(last, indices), nil
}

func buildBitpackPatchValues[T UnsignedInteger](values []T) (EncodedArray[T], error) {
	arr := array.NewPrimitivesUnsafe(values)
	if isAllSameUnsigned(values) {
		return newConstIntegerArray(arr)
	}
	return newRawArray(arr), nil
}

func isAllSameUnsigned[T UnsignedInteger](values []T) bool {
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

func (c *bitPackedArray[T]) Encoding() CodeType { return CodecTypeBitpack }
func (c *bitPackedArray[T]) Length() uint64     { return c.length }
func (c *bitPackedArray[T]) PType() PType       { return pTypeForType[T]() }
func (c *bitPackedArray[T]) BinarySize() uint64 {
	size := uint64(headerSize) + 1 + uint64(len(c.buf))
	if c.patches != nil {
		size += c.patches.BinarySize()
	}
	return size
}

func (c *bitPackedArray[T]) ValueAt(offset uint64) T {
	if offset >= c.length {
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

func (c *bitPackedArray[T]) decompress(dst []T) error {
	if c.bitWidth == 0 {
		var zero T
		for i := range dst {
			dst[i] = zero
		}
		return nil
	}
	for i := range dst {
		dst[i] = T(unpackUnsigned(c.buf, uint64(i)*uint64(c.bitWidth), c.bitWidth))
	}
	return c.patches.Apply(dst)
}

func (c *bitPackedArray[T]) Slice(start, end uint64) (EncodedArray[T], error) {
	if err := validateSliceBounds(c.length, start, end); err != nil {
		return nil, err
	}
	length := end - start
	sliced := &bitPackedArray[T]{length: length, bitWidth: c.bitWidth}
	if c.bitWidth > 0 && length > 0 {
		sliced.buf = make([]byte, packedByteSize(length, c.bitWidth))
		for i := uint64(0); i < length; i++ {
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

func (c *bitPackedArray[T]) WriteTo(w io.Writer) (int64, error) {
	flags := uint32(0)
	if c.patches != nil {
		flags |= flagBitpackHasPatches
	}
	n, err := header{
		Version:  versionNumber,
		Kind:     CodecTypeBitpack,
		ElemType: pTypeForType[T](),
		Flags:    flags,
		Length:   c.length,
		BodySize: 1 + uint64(len(c.buf)),
	}.WriteTo(w)
	if err != nil {
		return n, err
	}

	var width [1]byte
	width[0] = byte(c.bitWidth)
	nn, err := w.Write(width[:])
	n += int64(nn)
	if err != nil {
		return n, err
	}
	if nn != 1 {
		return n, io.ErrShortWrite
	}

	nn, err = w.Write(c.buf)
	n += int64(nn)
	if err != nil {
		return n, err
	}
	if nn != len(c.buf) {
		return n, io.ErrShortWrite
	}
	if c.patches != nil {
		nn64, err := c.patches.WriteTo(w)
		n += nn64
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

func readAnyBitPackedArray[T Integer | Float | String](r io.Reader, h header) (EncodedArray[T], error) {
	var zero T
	switch any(zero).(type) {
	case uint8:
		c, err := readBitPackedArray[uint8](r, h)
		if err != nil {
			return nil, err
		}
		return any(c).(EncodedArray[T]), nil
	case uint16:
		c, err := readBitPackedArray[uint16](r, h)
		if err != nil {
			return nil, err
		}
		return any(c).(EncodedArray[T]), nil
	case uint32:
		c, err := readBitPackedArray[uint32](r, h)
		if err != nil {
			return nil, err
		}
		return any(c).(EncodedArray[T]), nil
	case uint64:
		c, err := readBitPackedArray[uint64](r, h)
		if err != nil {
			return nil, err
		}
		return any(c).(EncodedArray[T]), nil
	default:
		return nil, fmt.Errorf("codec: bitpack not supported for %v", h.ElemType)
	}
}

func readBitpackPatchIndexCodec(r io.Reader) (ordinalArray, error) {
	h, err := readHeader(r)
	if err != nil {
		return nil, err
	}
	return readOrdinalArray(r, h)
}

func readBitPackedArray[T UnsignedInteger](r io.Reader, h header) (EncodedArray[T], error) {
	if h.BodySize < 1 {
		return nil, fmt.Errorf("codec: bitpack body too small")
	}

	var widthByte [1]byte
	if _, err := io.ReadFull(r, widthByte[:]); err != nil {
		return nil, err
	}
	bitWidth := uint(widthByte[0])
	maxBitWidth := uint(pTypeForType[T]().ByteWidth() * 8)
	if bitWidth > maxBitWidth {
		return nil, fmt.Errorf("codec: bit width = %d exceeds %T width", bitWidth, *new(T))
	}

	bufSize, err := checkedPackedByteSize(h.Length, bitWidth)
	if err != nil {
		return nil, err
	}
	if h.BodySize != 1+uint64(bufSize) {
		return nil, fmt.Errorf("codec: bitpack body size = %d, want %d", h.BodySize, 1+uint64(bufSize))
	}

	var buf []byte
	if bufSize > 0 {
		buf = make([]byte, bufSize)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
	}
	codec := &bitPackedArray[T]{length: h.Length, bitWidth: bitWidth, buf: buf}
	if h.Flags&flagBitpackHasPatches != 0 {
		var offsetBuf [8]byte
		if _, err := io.ReadFull(r, offsetBuf[:]); err != nil {
			return nil, err
		}
		offset := binary.LittleEndian.Uint64(offsetBuf[:])
		idxCodec, err := readBitpackPatchIndexCodec(r)
		if err != nil {
			return nil, err
		}
		valCodec, err := readEncodedArray[T](r)
		if err != nil {
			return nil, err
		}
		patches, err := newPatches(h.Length, offset, idxCodec, valCodec)
		if err != nil {
			return nil, prefixPatchError(err, "bitpack")
		}
		codec.patches = patches
	}
	return codec, nil
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
	for written := uint(0); written < bitWidth; {
		byteOffset := int(bitOffset / 8)
		bitInByte := uint(bitOffset % 8)
		take := minUint(bitWidth-written, 8-bitInByte)
		chunk := byte((value >> written) & uint64(maskForBits(take)))
		buf[byteOffset] |= chunk << bitInByte
		written += take
		bitOffset += uint64(take)
	}
}

func unpackUnsigned(buf []byte, bitOffset uint64, bitWidth uint) uint64 {
	var value uint64
	for read := uint(0); read < bitWidth; {
		byteOffset := int(bitOffset / 8)
		bitInByte := uint(bitOffset % 8)
		take := minUint(bitWidth-read, 8-bitInByte)
		chunk := (buf[byteOffset] >> bitInByte) & maskForBits(take)
		value |= uint64(chunk) << read
		read += take
		bitOffset += uint64(take)
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

func estimateBitpack[T UnsignedInteger, S statsSource[T]](stats S, ctx planContext) (float64, bool) {
	return estimateBySample(stats, ctx, buildBitPackedArray[T])
}
