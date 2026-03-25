package btrblocks

import (
	"encoding/binary"
	"fmt"
	"io"
	"math/bits"

	"github.com/axiomhq/btrblocks/array"
)

const flagBitpackHasPatches uint32 = 1 << 0

// bitPackedArray stores fixed-width packed unsigned values plus optional patches.
type bitPackedArray[T UnsignedInteger, I UnsignedInteger] struct {
	length   uint64
	bitWidth uint
	buf      []byte
	patches  *patches[T, I]
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

func unsignedBitWidthHistogram[T UnsignedInteger](arr interface {
	Length() uint64
	ValueAt(uint64) T
}) []uint64 {
	histogram := make([]uint64, array.PTypeForType[T]().ByteWidth()*8+1)
	for i := uint64(0); i < arr.Length(); i++ {
		histogram[bitWidthForUnsigned(uint64(arr.ValueAt(i)))]++
	}
	return histogram
}

func bytesPerBitpackException[T UnsignedInteger]() uint64 {
	return uint64(array.PTypeForType[T]().ByteWidth() + 4)
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

func buildBitPackedArray[T UnsignedInteger](arr array.ArrayCore[T], _ planContext) (EncodedArray[T], error) {
	bitWidth := findBestBitpackWidth[T](unsignedBitWidthHistogram(arr))
	return buildBitPackedArrayCore(arr, bitWidth)
}

// buildBitPackedArrayWithWidth builds a bitpacked array using a caller-supplied
// bit width, skipping the histogram pass. Used by FoR where the optimal width
// is already known from the min/max range.
func buildBitPackedArrayWithWidth[T UnsignedInteger](arr array.ArrayCore[T], bitWidth uint) (*bitPackedArray[T, uint64], error) {
	return buildBitPackedArrayCore(arr, bitWidth)
}

func buildBitPackedArrayCore[T UnsignedInteger](arr array.ArrayCore[T], bitWidth uint) (*bitPackedArray[T, uint64], error) {
	n := arr.Length()
	codec := &bitPackedArray[T, uint64]{length: n, bitWidth: bitWidth}
	if n == 0 {
		return codec, nil
	}

	if bitWidth > 0 {
		codec.buf = make([]byte, packedByteSize(n, bitWidth))
	}
	var patchIdx []uint64
	var patchVals []T
	for i := uint64(0); i < n; i++ {
		value := arr.ValueAt(i)
		v := uint64(value)
		if bitWidth > 0 {
			packUnsigned(codec.buf, i*uint64(bitWidth), bitWidth, v)
		}
		if bitWidthForUnsigned(v) > bitWidth {
			patchIdx = append(patchIdx, i)
			patchVals = append(patchVals, value)
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
	p, err := newPatches[T, uint64](n, 0, patchIdxCodec, patchValCodec)
	if err != nil {
		return nil, err
	}
	codec.patches = p
	return codec, nil
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

func (c *bitPackedArray[T, I]) Encoding() CodeType { return CodecTypeBitpack }
func (c *bitPackedArray[T, I]) Length() uint64      { return c.length }
func (c *bitPackedArray[T, I]) PType() PType        { return array.PTypeForType[T]() }
func (c *bitPackedArray[T, I]) BinarySize() uint64 {
	size := uint64(headerSize) + 1 + uint64(len(c.buf))
	if c.patches != nil {
		size += c.patches.BinarySize()
	}
	return size
}

func (c *bitPackedArray[T, I]) ValueAt(offset uint64) T {
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

func (c *bitPackedArray[T, I]) DecompressInto(dst []T) error {
	if err := checkDstLen(dst, c.length); err != nil {
		return err
	}
	if c.bitWidth == 0 {
		clear(dst[:c.length])
	} else {
		unpackBatchTyped(c.buf, c.length, c.bitWidth, dst[:c.length])
	}
	return c.patches.Apply(dst[:c.length])
}

// unpackBatchTyped decodes count packed values directly into a typed slice.
func unpackBatchTyped[T UnsignedInteger](buf []byte, count uint64, bitWidth uint, dst []T) {
	mask := uint64((1 << bitWidth) - 1)
	bitOff := uint64(0)
	bufLen := uint64(len(buf))
	for i := uint64(0); i < count; i++ {
		byteOff := bitOff >> 3
		bitInByte := uint(bitOff & 7)
		if byteOff+8 <= bufLen {
			word := binary.LittleEndian.Uint64(buf[byteOff:])
			dst[i] = T((word >> bitInByte) & mask)
		} else {
			dst[i] = T(unpackUnsigned(buf, bitOff, bitWidth))
		}
		bitOff += uint64(bitWidth)
	}
}



func (c *bitPackedArray[T, I]) Slice(start, end uint64) (EncodedArray[T], error) {
	if err := array.ValidateSliceBounds(c.length, start, end); err != nil {
		return nil, err
	}
	length := end - start
	sliced := &bitPackedArray[T, I]{length: length, bitWidth: c.bitWidth}
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

func (c *bitPackedArray[T, I]) WriteTo(w io.Writer) (int64, error) {
	flags := uint32(0)
	if c.patches != nil {
		flags |= flagBitpackHasPatches
	}
	n, err := header{
		Version:  versionNumber,
		Kind:     CodecTypeBitpack,
		ElemType: array.PTypeForType[T](),
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

func readAnyBitPackedArray[T Integer | Float | String](r io.Reader, h header, opts ReadOptions) (EncodedArray[T], error) {
	var zero T
	switch any(zero).(type) {
	case uint8:
		c, err := readBitPackedArray[uint8](r, h, opts)
		if err != nil {
			return nil, err
		}
		return any(c).(EncodedArray[T]), nil
	case uint16:
		c, err := readBitPackedArray[uint16](r, h, opts)
		if err != nil {
			return nil, err
		}
		return any(c).(EncodedArray[T]), nil
	case uint32:
		c, err := readBitPackedArray[uint32](r, h, opts)
		if err != nil {
			return nil, err
		}
		return any(c).(EncodedArray[T]), nil
	case uint64:
		c, err := readBitPackedArray[uint64](r, h, opts)
		if err != nil {
			return nil, err
		}
		return any(c).(EncodedArray[T]), nil
	default:
		return nil, fmt.Errorf("codec: bitpack not supported for %v", h.ElemType)
	}
}

func readBitPackedArray[T UnsignedInteger](r io.Reader, h header, opts ReadOptions) (EncodedArray[T], error) {
	if h.Flags&^flagBitpackHasPatches != 0 {
		return nil, fmt.Errorf("codec: unsupported bitpack flags = 0x%x", h.Flags)
	}
	if h.BodySize < 1 {
		return nil, fmt.Errorf("codec: bitpack body too small")
	}

	var widthByte [1]byte
	if _, err := io.ReadFull(r, widthByte[:]); err != nil {
		return nil, err
	}
	bitWidth := uint(widthByte[0])
	maxBitWidth := uint(array.PTypeForType[T]().ByteWidth() * 8)
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

	if h.Flags&flagBitpackHasPatches == 0 {
		return &bitPackedArray[T, uint64]{length: h.Length, bitWidth: bitWidth, buf: buf}, nil
	}

	var offsetBuf [8]byte
	if _, err := io.ReadFull(r, offsetBuf[:]); err != nil {
		return nil, err
	}
	offset := binary.LittleEndian.Uint64(offsetBuf[:])

	idxHeader, err := readHeader(r)
	if err != nil {
		return nil, err
	}
	switch idxHeader.ElemType {
	case PTypeUint8:
		return readBitPackedWithPatchIdx[T, uint8](r, h, opts, bitWidth, buf, offset, idxHeader)
	case PTypeUint16:
		return readBitPackedWithPatchIdx[T, uint16](r, h, opts, bitWidth, buf, offset, idxHeader)
	case PTypeUint32:
		return readBitPackedWithPatchIdx[T, uint32](r, h, opts, bitWidth, buf, offset, idxHeader)
	case PTypeUint64:
		return readBitPackedWithPatchIdx[T, uint64](r, h, opts, bitWidth, buf, offset, idxHeader)
	default:
		return nil, fmt.Errorf("codec: bitpack patch index type = %v, want unsigned integer", idxHeader.ElemType)
	}
}

func readBitPackedWithPatchIdx[T UnsignedInteger, I UnsignedInteger](r io.Reader, h header, opts ReadOptions, bitWidth uint, buf []byte, offset uint64, idxHeader header) (EncodedArray[T], error) {
	idxCodec, err := readEncodedArrayWithHeader[I](r, idxHeader, opts)
	if err != nil {
		return nil, err
	}
	valCodec, err := readEncodedArray[T](r, opts)
	if err != nil {
		return nil, err
	}
	p, err := newPatches[T, I](h.Length, offset, idxCodec, valCodec)
	if err != nil {
		return nil, prefixPatchError(err, "bitpack")
	}
	return &bitPackedArray[T, I]{length: h.Length, bitWidth: bitWidth, buf: buf, patches: p}, nil
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

func estimateBitpack[T UnsignedInteger, S statsSource[T]](stats S, ctx planContext) (float64, bool) {
	return estimateBySample(stats, ctx, buildBitPackedArray[T])
}
