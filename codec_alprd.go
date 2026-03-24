package btrblocks

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"math/bits"

	"github.com/axiomhq/btrblocks/array"
)

const (
	alprdMaxDictSize = 8
	alprdMaxCutBits  = 16
	flagALPRDHasPatches uint32 = 1 << 0
)

var errALPRDHighPatchRatio = fmt.Errorf("codec: ALPRD patch ratio exceeds 50%%")

// alprdDict holds the learned dictionary for ALP-RD encoding.
type alprdDict struct {
	rightBitWidth uint8
	leftBitWidth  uint8
	dictSize      uint8
	// forward: left-part value -> code (0..dictSize-1)
	forward map[uint16]uint8
	// reverse: code -> left-part value
	reverse [alprdMaxDictSize]uint16
}

func alprdFindBestDict64(arr array.Array[float64]) alprdDict {
	n := arr.Length()
	step := uint64(1)
	if n > 1024 {
		step = n / 1024
	}

	var best alprdDict
	bestCost := math.MaxFloat64

	for cut := uint8(1); cut <= alprdMaxCutBits; cut++ {
		rightBW := uint8(64) - cut

		// Count frequency of each left-part pattern in sample.
		freq := make(map[uint16]uint64)
		sampleN := uint64(0)
		for i := uint64(0); i < n; i += step {
			left := uint16(math.Float64bits(arr.ValueAt(i)) >> rightBW)
			freq[left]++
			sampleN++
		}

		// Find top-8 by frequency.
		type entry struct {
			left  uint16
			count uint64
		}
		top := make([]entry, 0, len(freq))
		for left, count := range freq {
			top = append(top, entry{left, count})
		}
		// Selection sort for top-8 (small constant bound).
		for i := 0; i < len(top) && i < alprdMaxDictSize; i++ {
			maxIdx := i
			for j := i + 1; j < len(top); j++ {
				if top[j].count > top[maxIdx].count {
					maxIdx = j
				}
			}
			top[i], top[maxIdx] = top[maxIdx], top[i]
		}

		dictSize := len(top)
		if dictSize > alprdMaxDictSize {
			dictSize = alprdMaxDictSize
		}

		// Count exceptions.
		dictSet := make(map[uint16]struct{}, dictSize)
		for i := 0; i < dictSize; i++ {
			dictSet[top[i].left] = struct{}{}
		}
		exceptions := uint64(0)
		for left, count := range freq {
			if _, ok := dictSet[left]; !ok {
				exceptions += count
			}
		}

		leftBW := uint8(bits.Len(uint(dictSize) - 1))
		if leftBW == 0 {
			leftBW = 1
		}

		// Cost: bits per value + exception overhead amortized.
		cost := float64(rightBW) + float64(leftBW) + float64(exceptions)*32.0/float64(sampleN)
		if cost < bestCost {
			bestCost = cost
			fwd := make(map[uint16]uint8, dictSize)
			var rev [alprdMaxDictSize]uint16
			for i := 0; i < dictSize; i++ {
				fwd[top[i].left] = uint8(i)
				rev[i] = top[i].left
			}
			best = alprdDict{
				rightBitWidth: rightBW,
				leftBitWidth:  leftBW,
				dictSize:      uint8(dictSize),
				forward:       fwd,
				reverse:       rev,
			}
		}
	}
	return best
}

func alprdFindBestDict32(arr array.Array[float32]) alprdDict {
	n := arr.Length()
	step := uint64(1)
	if n > 1024 {
		step = n / 1024
	}

	var best alprdDict
	bestCost := math.MaxFloat64

	for cut := uint8(1); cut <= alprdMaxCutBits; cut++ {
		rightBW := uint8(32) - cut

		freq := make(map[uint16]uint64)
		sampleN := uint64(0)
		for i := uint64(0); i < n; i += step {
			left := uint16(math.Float32bits(arr.ValueAt(i)) >> rightBW)
			freq[left]++
			sampleN++
		}

		type entry struct {
			left  uint16
			count uint64
		}
		top := make([]entry, 0, len(freq))
		for left, count := range freq {
			top = append(top, entry{left, count})
		}
		for i := 0; i < len(top) && i < alprdMaxDictSize; i++ {
			maxIdx := i
			for j := i + 1; j < len(top); j++ {
				if top[j].count > top[maxIdx].count {
					maxIdx = j
				}
			}
			top[i], top[maxIdx] = top[maxIdx], top[i]
		}

		dictSize := len(top)
		if dictSize > alprdMaxDictSize {
			dictSize = alprdMaxDictSize
		}

		dictSet := make(map[uint16]struct{}, dictSize)
		for i := 0; i < dictSize; i++ {
			dictSet[top[i].left] = struct{}{}
		}
		exceptions := uint64(0)
		for left, count := range freq {
			if _, ok := dictSet[left]; !ok {
				exceptions += count
			}
		}

		leftBW := uint8(bits.Len(uint(dictSize) - 1))
		if leftBW == 0 {
			leftBW = 1
		}

		cost := float64(rightBW) + float64(leftBW) + float64(exceptions)*32.0/float64(sampleN)
		if cost < bestCost {
			bestCost = cost
			fwd := make(map[uint16]uint8, dictSize)
			var rev [alprdMaxDictSize]uint16
			for i := 0; i < dictSize; i++ {
				fwd[top[i].left] = uint8(i)
				rev[i] = top[i].left
			}
			best = alprdDict{
				rightBitWidth: rightBW,
				leftBitWidth:  leftBW,
				dictSize:      uint8(dictSize),
				forward:       fwd,
				reverse:       rev,
			}
		}
	}
	return best
}

// alprdArray64 stores ALP-RD encoded float64 values.
type alprdArray64 struct {
	length        uint64
	rightBitWidth uint8
	dictSize      uint8
	dict          [alprdMaxDictSize]uint16
	leftParts     []byte // bit-packed left codes
	leftBitWidth  uint8
	rightParts    []byte // bit-packed right parts
	patches       *patches[uint16]
}

// alprdBodySize returns the fixed body size for the header.
// Body: rightBitWidth(1) + leftBitWidth(1) + dictSize(1) + dict(dictSize*2) + leftLen(4) + rightLen(4)
func alprdBodySize64(dictSize uint8, leftBuf, rightBuf []byte) uint64 {
	return 1 + 1 + 1 + uint64(dictSize)*2 + 4 + uint64(len(leftBuf)) + 4 + uint64(len(rightBuf))
}

func (a *alprdArray64) Encoding() CodeType { return CodecTypeALPRD }
func (a *alprdArray64) Length() uint64      { return a.length }
func (a *alprdArray64) PType() PType        { return pTypeForType[float64]() }
func (a *alprdArray64) BinarySize() uint64 {
	size := uint64(headerSize) + alprdBodySize64(a.dictSize, a.leftParts, a.rightParts)
	if a.patches != nil {
		size += a.patches.BinarySize()
	}
	return size
}

func (a *alprdArray64) alprdDecode64(offset uint64) float64 {
	code := unpackUnsigned(a.leftParts, offset*uint64(a.leftBitWidth), uint(a.leftBitWidth))
	left := uint64(a.dict[code])
	if leftOverride, ok := a.patches.ValueAt(offset); ok {
		left = uint64(leftOverride)
	}
	right := unpackUnsigned(a.rightParts, offset*uint64(a.rightBitWidth), uint(a.rightBitWidth))
	return math.Float64frombits((left << a.rightBitWidth) | right)
}

func (a *alprdArray64) ValueAt(offset uint64) float64 {
	if offset >= a.length {
		panic(errOffsetOutOfRange)
	}
	return a.alprdDecode64(offset)
}

func (a *alprdArray64) Decompress() ([]float64, error) {
	dst := make([]float64, a.length)
	for i := uint64(0); i < a.length; i++ {
		code := unpackUnsigned(a.leftParts, i*uint64(a.leftBitWidth), uint(a.leftBitWidth))
		left := uint64(a.dict[code])
		right := unpackUnsigned(a.rightParts, i*uint64(a.rightBitWidth), uint(a.rightBitWidth))
		dst[i] = math.Float64frombits((left << a.rightBitWidth) | right)
	}
	if a.patches != nil {
		for i := uint64(0); i < a.patches.indices.Length(); i++ {
			idx := a.patches.indices.ValueAt(i) - a.patches.offset
			left := uint64(a.patches.values.ValueAt(i))
			right := unpackUnsigned(a.rightParts, idx*uint64(a.rightBitWidth), uint(a.rightBitWidth))
			dst[idx] = math.Float64frombits((left << a.rightBitWidth) | right)
		}
	}
	return dst, nil
}

func (a *alprdArray64) Slice(start, end uint64) (EncodedArray[float64], error) {
	if err := validateSliceBounds(a.length, start, end); err != nil {
		return nil, err
	}
	length := end - start
	sliced := &alprdArray64{
		length:        length,
		rightBitWidth: a.rightBitWidth,
		dictSize:      a.dictSize,
		dict:          a.dict,
		leftBitWidth:  a.leftBitWidth,
	}
	if length > 0 {
		sliced.leftParts = make([]byte, packedByteSize(length, uint(a.leftBitWidth)))
		for i := uint64(0); i < length; i++ {
			v := unpackUnsigned(a.leftParts, (start+i)*uint64(a.leftBitWidth), uint(a.leftBitWidth))
			packUnsigned(sliced.leftParts, i*uint64(a.leftBitWidth), uint(a.leftBitWidth), v)
		}
		sliced.rightParts = make([]byte, packedByteSize(length, uint(a.rightBitWidth)))
		for i := uint64(0); i < length; i++ {
			v := unpackUnsigned(a.rightParts, (start+i)*uint64(a.rightBitWidth), uint(a.rightBitWidth))
			packUnsigned(sliced.rightParts, i*uint64(a.rightBitWidth), uint(a.rightBitWidth), v)
		}
	}
	patches, err := a.patches.Slice(start, end)
	if err != nil {
		return nil, err
	}
	sliced.patches = patches
	return sliced, nil
}

func (a *alprdArray64) WriteTo(w io.Writer) (int64, error) {
	flags := uint32(0)
	if a.patches != nil {
		flags |= flagALPRDHasPatches
	}
	bodySize := alprdBodySize64(a.dictSize, a.leftParts, a.rightParts)
	n, err := header{
		Version:  versionNumber,
		Kind:     CodecTypeALPRD,
		ElemType: pTypeForType[float64](),
		Flags:    flags,
		Length:   a.length,
		BodySize: bodySize,
	}.WriteTo(w)
	if err != nil {
		return n, err
	}

	// Body: rightBitWidth, leftBitWidth, dictSize, dict entries, left packed, right packed.
	buf := make([]byte, bodySize)
	off := 0
	buf[off] = a.rightBitWidth
	off++
	buf[off] = a.leftBitWidth
	off++
	buf[off] = a.dictSize
	off++
	for i := uint8(0); i < a.dictSize; i++ {
		binary.LittleEndian.PutUint16(buf[off:], a.dict[i])
		off += 2
	}
	binary.LittleEndian.PutUint32(buf[off:], uint32(len(a.leftParts)))
	off += 4
	copy(buf[off:], a.leftParts)
	off += len(a.leftParts)
	binary.LittleEndian.PutUint32(buf[off:], uint32(len(a.rightParts)))
	off += 4
	copy(buf[off:], a.rightParts)

	nn, err := w.Write(buf)
	n += int64(nn)
	if err != nil {
		return n, err
	}
	if nn != len(buf) {
		return n, io.ErrShortWrite
	}

	if a.patches != nil {
		nn64, err := a.patches.WriteTo(w)
		n += nn64
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// alprdArray32 stores ALP-RD encoded float32 values.
type alprdArray32 struct {
	length        uint64
	rightBitWidth uint8
	dictSize      uint8
	dict          [alprdMaxDictSize]uint16
	leftParts     []byte
	leftBitWidth  uint8
	rightParts    []byte
	patches       *patches[uint16]
}

func alprdBodySize32(dictSize uint8, leftBuf, rightBuf []byte) uint64 {
	return 1 + 1 + 1 + uint64(dictSize)*2 + 4 + uint64(len(leftBuf)) + 4 + uint64(len(rightBuf))
}

func (a *alprdArray32) Encoding() CodeType { return CodecTypeALPRD }
func (a *alprdArray32) Length() uint64      { return a.length }
func (a *alprdArray32) PType() PType        { return pTypeForType[float32]() }
func (a *alprdArray32) BinarySize() uint64 {
	size := uint64(headerSize) + alprdBodySize32(a.dictSize, a.leftParts, a.rightParts)
	if a.patches != nil {
		size += a.patches.BinarySize()
	}
	return size
}

func (a *alprdArray32) alprdDecode32(offset uint64) float32 {
	code := unpackUnsigned(a.leftParts, offset*uint64(a.leftBitWidth), uint(a.leftBitWidth))
	left := uint32(a.dict[code])
	if leftOverride, ok := a.patches.ValueAt(offset); ok {
		left = uint32(leftOverride)
	}
	right := uint32(unpackUnsigned(a.rightParts, offset*uint64(a.rightBitWidth), uint(a.rightBitWidth)))
	return math.Float32frombits((left << a.rightBitWidth) | right)
}

func (a *alprdArray32) ValueAt(offset uint64) float32 {
	if offset >= a.length {
		panic(errOffsetOutOfRange)
	}
	return a.alprdDecode32(offset)
}

func (a *alprdArray32) Decompress() ([]float32, error) {
	dst := make([]float32, a.length)
	for i := uint64(0); i < a.length; i++ {
		code := unpackUnsigned(a.leftParts, i*uint64(a.leftBitWidth), uint(a.leftBitWidth))
		left := uint32(a.dict[code])
		right := uint32(unpackUnsigned(a.rightParts, i*uint64(a.rightBitWidth), uint(a.rightBitWidth)))
		dst[i] = math.Float32frombits((left << a.rightBitWidth) | right)
	}
	if a.patches != nil {
		for i := uint64(0); i < a.patches.indices.Length(); i++ {
			idx := a.patches.indices.ValueAt(i) - a.patches.offset
			left := uint32(a.patches.values.ValueAt(i))
			right := uint32(unpackUnsigned(a.rightParts, idx*uint64(a.rightBitWidth), uint(a.rightBitWidth)))
			dst[idx] = math.Float32frombits((left << a.rightBitWidth) | right)
		}
	}
	return dst, nil
}

func (a *alprdArray32) Slice(start, end uint64) (EncodedArray[float32], error) {
	if err := validateSliceBounds(a.length, start, end); err != nil {
		return nil, err
	}
	length := end - start
	sliced := &alprdArray32{
		length:        length,
		rightBitWidth: a.rightBitWidth,
		dictSize:      a.dictSize,
		dict:          a.dict,
		leftBitWidth:  a.leftBitWidth,
	}
	if length > 0 {
		sliced.leftParts = make([]byte, packedByteSize(length, uint(a.leftBitWidth)))
		for i := uint64(0); i < length; i++ {
			v := unpackUnsigned(a.leftParts, (start+i)*uint64(a.leftBitWidth), uint(a.leftBitWidth))
			packUnsigned(sliced.leftParts, i*uint64(a.leftBitWidth), uint(a.leftBitWidth), v)
		}
		sliced.rightParts = make([]byte, packedByteSize(length, uint(a.rightBitWidth)))
		for i := uint64(0); i < length; i++ {
			v := unpackUnsigned(a.rightParts, (start+i)*uint64(a.rightBitWidth), uint(a.rightBitWidth))
			packUnsigned(sliced.rightParts, i*uint64(a.rightBitWidth), uint(a.rightBitWidth), v)
		}
	}
	patches, err := a.patches.Slice(start, end)
	if err != nil {
		return nil, err
	}
	sliced.patches = patches
	return sliced, nil
}

func (a *alprdArray32) WriteTo(w io.Writer) (int64, error) {
	flags := uint32(0)
	if a.patches != nil {
		flags |= flagALPRDHasPatches
	}
	bodySize := alprdBodySize32(a.dictSize, a.leftParts, a.rightParts)
	n, err := header{
		Version:  versionNumber,
		Kind:     CodecTypeALPRD,
		ElemType: pTypeForType[float32](),
		Flags:    flags,
		Length:   a.length,
		BodySize: bodySize,
	}.WriteTo(w)
	if err != nil {
		return n, err
	}

	buf := make([]byte, bodySize)
	off := 0
	buf[off] = a.rightBitWidth
	off++
	buf[off] = a.leftBitWidth
	off++
	buf[off] = a.dictSize
	off++
	for i := uint8(0); i < a.dictSize; i++ {
		binary.LittleEndian.PutUint16(buf[off:], a.dict[i])
		off += 2
	}
	binary.LittleEndian.PutUint32(buf[off:], uint32(len(a.leftParts)))
	off += 4
	copy(buf[off:], a.leftParts)
	off += len(a.leftParts)
	binary.LittleEndian.PutUint32(buf[off:], uint32(len(a.rightParts)))
	off += 4
	copy(buf[off:], a.rightParts)

	nn, err := w.Write(buf)
	n += int64(nn)
	if err != nil {
		return n, err
	}
	if nn != len(buf) {
		return n, io.ErrShortWrite
	}

	if a.patches != nil {
		nn64, err := a.patches.WriteTo(w)
		n += nn64
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// buildALPRDArray compresses a float array using ALP-RD.
func buildALPRDArray[T Float](arr array.Array[T], ctx planContext) (EncodedArray[T], error) {
	if ctx.depth <= 0 {
		return nil, errDepthExhausted
	}
	if arr.Length() == 0 {
		return nil, errDataEmpty
	}

	var zero T
	switch any(zero).(type) {
	case float64:
		c, err := buildALPRDArray64(any(arr).(array.Array[float64]), ctx)
		if err != nil {
			return nil, err
		}
		return any(c).(EncodedArray[T]), nil
	case float32:
		c, err := buildALPRDArray32(any(arr).(array.Array[float32]), ctx)
		if err != nil {
			return nil, err
		}
		return any(c).(EncodedArray[T]), nil
	default:
		return nil, fmt.Errorf("codec: ALPRD not supported for %T", zero)
	}
}

func buildALPRDArray64(arr array.Array[float64], ctx planContext) (EncodedArray[float64], error) {
	dict := alprdFindBestDict64(arr)
	n := arr.Length()

	leftBuf := make([]byte, packedByteSize(n, uint(dict.leftBitWidth)))
	rightBuf := make([]byte, packedByteSize(n, uint(dict.rightBitWidth)))

	patchIdx := make([]uint64, 0)
	patchVals := make([]uint16, 0)

	for i := uint64(0); i < n; i++ {
		value := arr.ValueAt(i)
		b := math.Float64bits(value)
		right := b & ((1 << dict.rightBitWidth) - 1)
		left := uint16(b >> dict.rightBitWidth)

		code, ok := dict.forward[left]
		if !ok {
			// Exception: store left-part as patch, write code 0 as placeholder.
			patchIdx = append(patchIdx, i)
			patchVals = append(patchVals, left)
			code = 0
		}

		packUnsigned(leftBuf, i*uint64(dict.leftBitWidth), uint(dict.leftBitWidth), uint64(code))
		packUnsigned(rightBuf, i*uint64(dict.rightBitWidth), uint(dict.rightBitWidth), right)
	}

	if uint64(len(patchIdx))*2 > n {
		return nil, errALPRDHighPatchRatio
	}

	codec := &alprdArray64{
		length:        n,
		rightBitWidth: dict.rightBitWidth,
		dictSize:      dict.dictSize,
		dict:          dict.reverse,
		leftParts:     leftBuf,
		leftBitWidth:  dict.leftBitWidth,
		rightParts:    rightBuf,
	}

	if len(patchIdx) > 0 {
		patches, err := buildALPRDPatchesU16(n, patchIdx, patchVals, ctx)
		if err != nil {
			return nil, err
		}
		codec.patches = patches
	}

	return any(codec).(EncodedArray[float64]), nil
}

func buildALPRDArray32(arr array.Array[float32], ctx planContext) (EncodedArray[float32], error) {
	dict := alprdFindBestDict32(arr)
	n := arr.Length()

	leftBuf := make([]byte, packedByteSize(n, uint(dict.leftBitWidth)))
	rightBuf := make([]byte, packedByteSize(n, uint(dict.rightBitWidth)))

	patchIdx := make([]uint64, 0)
	patchVals := make([]uint16, 0)

	for i := uint64(0); i < n; i++ {
		value := arr.ValueAt(i)
		b := math.Float32bits(value)
		right := uint64(b & ((1 << dict.rightBitWidth) - 1))
		left := uint16(b >> dict.rightBitWidth)

		code, ok := dict.forward[left]
		if !ok {
			patchIdx = append(patchIdx, i)
			patchVals = append(patchVals, left)
			code = 0
		}

		packUnsigned(leftBuf, i*uint64(dict.leftBitWidth), uint(dict.leftBitWidth), uint64(code))
		packUnsigned(rightBuf, i*uint64(dict.rightBitWidth), uint(dict.rightBitWidth), right)
	}

	if uint64(len(patchIdx))*2 > n {
		return nil, errALPRDHighPatchRatio
	}

	codec := &alprdArray32{
		length:        n,
		rightBitWidth: dict.rightBitWidth,
		dictSize:      dict.dictSize,
		dict:          dict.reverse,
		leftParts:     leftBuf,
		leftBitWidth:  dict.leftBitWidth,
		rightParts:    rightBuf,
	}

	if len(patchIdx) > 0 {
		patches, err := buildALPRDPatchesU16(n, patchIdx, patchVals, ctx)
		if err != nil {
			return nil, err
		}
		codec.patches = patches
	}

	return any(codec).(EncodedArray[float32]), nil
}

func buildALPRDPatchesU16(length uint64, patchIdx []uint64, patchVals []uint16, ctx planContext) (*patches[uint16], error) {
	patchIdxCodec, err := buildCompressedOrdinals(patchIdx, ctx.descend(), CodecTypeDict, CodecTypeRunEnd)
	if err != nil {
		return nil, err
	}
	if isAllSameUnsigned(patchVals) {
		patchValCodec, err := newConstIntegerArray(array.NewPrimitivesUnsafe(patchVals))
		if err != nil {
			return nil, err
		}
		return newPatches(length, 0, patchIdxCodec, patchValCodec)
	}
	return newPatches(length, 0, patchIdxCodec, newRawArray(array.NewPrimitivesUnsafe(patchVals)))
}

func estimateALPRD[T Float, S statsSource[T]](isConst bool) func(S, planContext) (float64, bool) {
	return func(stats S, ctx planContext) (float64, bool) {
		if ctx.depth <= 0 || isConst {
			return 0, false
		}
		return estimateBySample(stats, ctx, buildALPRDArray[T])
	}
}

// readAnyALPRDArray dispatches deserialization by float type.
func readAnyALPRDArray[T Integer | Float | String](r io.Reader, h header) (EncodedArray[T], error) {
	var zero T
	switch any(zero).(type) {
	case float64:
		c, err := readALPRDArray64(r, h)
		if err != nil {
			return nil, err
		}
		return any(c).(EncodedArray[T]), nil
	case float32:
		c, err := readALPRDArray32(r, h)
		if err != nil {
			return nil, err
		}
		return any(c).(EncodedArray[T]), nil
	default:
		return nil, fmt.Errorf("codec: ALPRD not supported for %v", h.ElemType)
	}
}

func readALPRDArray64(r io.Reader, h header) (EncodedArray[float64], error) {
	// Read body bytes.
	body := make([]byte, h.BodySize)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}

	off := 0
	if off >= len(body) {
		return nil, fmt.Errorf("codec: ALPRD body too small")
	}
	rightBitWidth := body[off]
	off++
	leftBitWidth := body[off]
	off++
	dictSize := body[off]
	off++

	if dictSize > alprdMaxDictSize {
		return nil, fmt.Errorf("codec: ALPRD dict size = %d, max %d", dictSize, alprdMaxDictSize)
	}

	var dict [alprdMaxDictSize]uint16
	for i := uint8(0); i < dictSize; i++ {
		dict[i] = binary.LittleEndian.Uint16(body[off:])
		off += 2
	}

	leftLen := binary.LittleEndian.Uint32(body[off:])
	off += 4
	leftParts := make([]byte, leftLen)
	copy(leftParts, body[off:off+int(leftLen)])
	off += int(leftLen)

	rightLen := binary.LittleEndian.Uint32(body[off:])
	off += 4
	rightParts := make([]byte, rightLen)
	copy(rightParts, body[off:off+int(rightLen)])

	codec := &alprdArray64{
		length:        h.Length,
		rightBitWidth: rightBitWidth,
		dictSize:      dictSize,
		dict:          dict,
		leftParts:     leftParts,
		leftBitWidth:  leftBitWidth,
		rightParts:    rightParts,
	}

	if h.Flags&flagALPRDHasPatches != 0 {
		var offsetBuf [8]byte
		if _, err := io.ReadFull(r, offsetBuf[:]); err != nil {
			return nil, err
		}
		offset := binary.LittleEndian.Uint64(offsetBuf[:])
		idxHeader, err := readHeader(r)
		if err != nil {
			return nil, err
		}
		idxCodec, err := readOrdinalArray(r, idxHeader)
		if err != nil {
			return nil, err
		}
		valCodec, err := readEncodedArray[uint16](r)
		if err != nil {
			return nil, err
		}
		patches, err := newPatches(h.Length, offset, idxCodec, valCodec)
		if err != nil {
			return nil, prefixPatchError(err, "ALPRD")
		}
		codec.patches = patches
	}
	return codec, nil
}

func readALPRDArray32(r io.Reader, h header) (EncodedArray[float32], error) {
	body := make([]byte, h.BodySize)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}

	off := 0
	if off >= len(body) {
		return nil, fmt.Errorf("codec: ALPRD body too small")
	}
	rightBitWidth := body[off]
	off++
	leftBitWidth := body[off]
	off++
	dictSize := body[off]
	off++

	if dictSize > alprdMaxDictSize {
		return nil, fmt.Errorf("codec: ALPRD dict size = %d, max %d", dictSize, alprdMaxDictSize)
	}

	var dict [alprdMaxDictSize]uint16
	for i := uint8(0); i < dictSize; i++ {
		dict[i] = binary.LittleEndian.Uint16(body[off:])
		off += 2
	}

	leftLen := binary.LittleEndian.Uint32(body[off:])
	off += 4
	leftParts := make([]byte, leftLen)
	copy(leftParts, body[off:off+int(leftLen)])
	off += int(leftLen)

	rightLen := binary.LittleEndian.Uint32(body[off:])
	off += 4
	rightParts := make([]byte, rightLen)
	copy(rightParts, body[off:off+int(rightLen)])

	codec := &alprdArray32{
		length:        h.Length,
		rightBitWidth: rightBitWidth,
		dictSize:      dictSize,
		dict:          dict,
		leftParts:     leftParts,
		leftBitWidth:  leftBitWidth,
		rightParts:    rightParts,
	}

	if h.Flags&flagALPRDHasPatches != 0 {
		var offsetBuf [8]byte
		if _, err := io.ReadFull(r, offsetBuf[:]); err != nil {
			return nil, err
		}
		offset := binary.LittleEndian.Uint64(offsetBuf[:])
		idxHeader, err := readHeader(r)
		if err != nil {
			return nil, err
		}
		idxCodec, err := readOrdinalArray(r, idxHeader)
		if err != nil {
			return nil, err
		}
		valCodec, err := readEncodedArray[uint16](r)
		if err != nil {
			return nil, err
		}
		patches, err := newPatches(h.Length, offset, idxCodec, valCodec)
		if err != nil {
			return nil, prefixPatchError(err, "ALPRD")
		}
		codec.patches = patches
	}
	return codec, nil
}
