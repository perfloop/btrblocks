package btrblocks

import (
	"fmt"
	"io"
	"math/bits"

	"github.com/axiomhq/btrblocks/array"
)

type bitpackCodec[T UnsignedInteger] struct {
	length    uint64
	bitWidth  uint
	buf       []byte
	patchIdxC patchIndexCodec
	patchValC Codec[T]
}

type patchIndexCodec interface {
	io.WriterTo
	BinarySize() uint64
	Length() uint64
	ValueAt(offset uint64) uint64
}

type patchIndexView[T UnsignedInteger] struct {
	codec Codec[T]
}

func (p patchIndexView[T]) WriteTo(w io.Writer) (int64, error) {
	return p.codec.WriteTo(w)
}

func (p patchIndexView[T]) BinarySize() uint64 {
	return p.codec.BinarySize()
}

func (p patchIndexView[T]) Length() uint64 {
	return p.codec.Length()
}

func (p patchIndexView[T]) ValueAt(offset uint64) uint64 {
	return uint64(p.codec.ValueAt(offset))
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

func newBitpackCodecAtWidth[T UnsignedInteger](arr interface {
	Length() uint64
	ValueAt(uint64) T
}, bitWidth uint) *bitpackCodec[T] {
	codec := &bitpackCodec[T]{length: arr.Length(), bitWidth: bitWidth}
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

func newBitpackCodec[T UnsignedInteger](arr interface {
	Length() uint64
	ValueAt(uint64) T
}) *bitpackCodec[T] {
	var max uint64
	for i := uint64(0); i < arr.Length(); i++ {
		if value := uint64(arr.ValueAt(i)); value > max {
			max = value
		}
	}
	return newBitpackCodecAtWidth(arr, bitWidthForUnsigned(max))
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

func buildBitpackCodec[T UnsignedInteger](arr array.Array[T], _ planContext) (Codec[T], error) {
	bitWidth := findBestBitpackWidth[T](unsignedBitWidthHistogram(arr))
	codec := newBitpackCodecAtWidth(arr, bitWidth)

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
	codec.patchIdxC = patchIdxCodec
	codec.patchValC = patchValCodec
	return codec, nil
}

func buildBitpackPatchIndices(indices []uint64) (patchIndexCodec, error) {
	last := indices[len(indices)-1]
	switch {
	case last <= uint64(^uint8(0)):
		narrow := make([]uint8, len(indices))
		for i, idx := range indices {
			narrow[i] = uint8(idx)
		}
		return patchIndexView[uint8]{codec: newRawCodec(array.NewPrimitivesUnsafe(narrow))}, nil
	case last <= uint64(^uint16(0)):
		narrow := make([]uint16, len(indices))
		for i, idx := range indices {
			narrow[i] = uint16(idx)
		}
		return patchIndexView[uint16]{codec: newRawCodec(array.NewPrimitivesUnsafe(narrow))}, nil
	case last <= uint64(^uint32(0)):
		narrow := make([]uint32, len(indices))
		for i, idx := range indices {
			narrow[i] = uint32(idx)
		}
		return patchIndexView[uint32]{codec: newRawCodec(array.NewPrimitivesUnsafe(narrow))}, nil
	default:
		narrow := make([]uint64, len(indices))
		copy(narrow, indices)
		return patchIndexView[uint64]{codec: newRawCodec(array.NewPrimitivesUnsafe(narrow))}, nil
	}
}

func buildBitpackPatchValues[T UnsignedInteger](values []T) (Codec[T], error) {
	arr := array.NewPrimitivesUnsafe(values)
	if isAllSameUnsigned(values) {
		return newConstIntegerCodec(arr)
	}
	return newRawCodec(arr), nil
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

func (c *bitpackCodec[T]) Kind() CodeType { return CodecTypeBitpack }
func (c *bitpackCodec[T]) Length() uint64 { return c.length }
func (c *bitpackCodec[T]) PType() PType   { return pTypeForType[T]() }
func (c *bitpackCodec[T]) BinarySize() uint64 {
	size := uint64(headerSize) + 1 + uint64(len(c.buf))
	if c.patchIdxC != nil {
		size += c.patchIdxC.BinarySize() + c.patchValC.BinarySize()
	}
	return size
}

func findBitpackPatchIndex(idxCodec patchIndexCodec, offset uint64) (uint64, bool) {
	if idxCodec == nil {
		return 0, false
	}
	lo, hi := uint64(0), idxCodec.Length()
	for lo < hi {
		mid := lo + (hi-lo)/2
		if idxCodec.ValueAt(mid) < offset {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo < idxCodec.Length() && idxCodec.ValueAt(lo) == offset {
		return lo, true
	}
	return 0, false
}

func (c *bitpackCodec[T]) ValueAt(offset uint64) T {
	if offset >= c.length {
		panic(errOffsetOutOfRange)
	}
	if idx, ok := findBitpackPatchIndex(c.patchIdxC, offset); ok {
		return c.patchValC.ValueAt(idx)
	}
	if c.bitWidth == 0 {
		var zero T
		return zero
	}
	return T(unpackUnsigned(c.buf, offset*uint64(c.bitWidth), c.bitWidth))
}

func (c *bitpackCodec[T]) Decode(dst []T) error {
	if err := validateDecodeLength(c.length, len(dst)); err != nil {
		return err
	}
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
	if c.patchIdxC != nil {
		for i := uint64(0); i < c.patchIdxC.Length(); i++ {
			dst[int(c.patchIdxC.ValueAt(i))] = c.patchValC.ValueAt(i)
		}
	}
	return nil
}

func (c *bitpackCodec[T]) WriteTo(w io.Writer) (int64, error) {
	flags := uint32(0)
	if c.patchIdxC != nil {
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
	if c.patchIdxC != nil {
		nn64, err := c.patchIdxC.WriteTo(w)
		n += nn64
		if err != nil {
			return n, err
		}
		nn64, err = c.patchValC.WriteTo(w)
		n += nn64
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

func readAnyBitpackCodec[T Integer | Float | String](r io.Reader, h header) (Codec[T], error) {
	var zero T
	switch any(zero).(type) {
	case uint8:
		c, err := readBitpackCodec[uint8](r, h)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	case uint16:
		c, err := readBitpackCodec[uint16](r, h)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	case uint32:
		c, err := readBitpackCodec[uint32](r, h)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	case uint64:
		c, err := readBitpackCodec[uint64](r, h)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	default:
		return nil, fmt.Errorf("codec: bitpack not supported for %v", h.ElemType)
	}
}

func readBitpackPatchIndexCodec(r io.Reader) (patchIndexCodec, error) {
	h, err := readHeader(r)
	if err != nil {
		return nil, err
	}
	switch h.ElemType {
	case PTypeUint8:
		c, err := readCodecWithHeader[uint8](r, h)
		if err != nil {
			return nil, err
		}
		return patchIndexView[uint8]{codec: c}, nil
	case PTypeUint16:
		c, err := readCodecWithHeader[uint16](r, h)
		if err != nil {
			return nil, err
		}
		return patchIndexView[uint16]{codec: c}, nil
	case PTypeUint32:
		c, err := readCodecWithHeader[uint32](r, h)
		if err != nil {
			return nil, err
		}
		return patchIndexView[uint32]{codec: c}, nil
	case PTypeUint64:
		c, err := readCodecWithHeader[uint64](r, h)
		if err != nil {
			return nil, err
		}
		return patchIndexView[uint64]{codec: c}, nil
	default:
		return nil, fmt.Errorf("codec: bitpack patch index type = %v, want unsigned integer", h.ElemType)
	}
}

func readBitpackCodec[T UnsignedInteger](r io.Reader, h header) (Codec[T], error) {
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
	codec := &bitpackCodec[T]{length: h.Length, bitWidth: bitWidth, buf: buf}
	if h.Flags&flagBitpackHasPatches != 0 {
		idxCodec, err := readBitpackPatchIndexCodec(r)
		if err != nil {
			return nil, err
		}
		valCodec, err := readCodec[T](r)
		if err != nil {
			return nil, err
		}
		if idxCodec.Length() != valCodec.Length() {
			return nil, fmt.Errorf("codec: bitpack patch length mismatch %d vs %d", idxCodec.Length(), valCodec.Length())
		}
		if idxCodec.Length() == 0 {
			return nil, fmt.Errorf("codec: bitpack patches length = 0")
		}
		var prev uint64
		for i := uint64(0); i < idxCodec.Length(); i++ {
			idx := idxCodec.ValueAt(i)
			if idx >= h.Length {
				return nil, fmt.Errorf("codec: bitpack patch index = %d, want < %d", idx, h.Length)
			}
			if i > 0 && idx <= prev {
				return nil, fmt.Errorf("codec: bitpack patch index = %d, want > %d", idx, prev)
			}
			prev = idx
		}
		codec.patchIdxC = idxCodec
		codec.patchValC = valCodec
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
	return estimateBySample(stats, ctx, buildBitpackCodec[T])
}
