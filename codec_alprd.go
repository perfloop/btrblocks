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
	alprdMaxDictSize           = 8
	alprdMaxCutBits            = 16
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

// alprdFuncs bundles width-specific float bit operations for ALP-RD.
type alprdFuncs[T Float] struct {
	floatBits     func(T) uint64
	floatFromBits func(uint64) T
	totalBits     uint8
}

var alprdFuncs64 = alprdFuncs[float64]{
	floatBits:     math.Float64bits,
	floatFromBits: math.Float64frombits,
	totalBits:     64,
}

var alprdFuncs32 = alprdFuncs[float32]{
	floatBits:     func(v float32) uint64 { return uint64(math.Float32bits(v)) },
	floatFromBits: func(b uint64) float32 { return math.Float32frombits(uint32(b)) },
	totalBits:     32,
}

func alprdFindBestDict[T Float](arr array.ArrayCore[T], funcs alprdFuncs[T]) alprdDict {
	n := arr.Length()
	step := uint64(1)
	if n > 1024 {
		step = n / 1024
	}

	var best alprdDict
	bestCost := math.MaxFloat64
	freq := make(map[uint16]uint64)

	for cut := uint8(1); cut <= alprdMaxCutBits; cut++ {
		rightBW := funcs.totalBits - cut

		clear(freq)
		sampleN := uint64(0)
		for i := uint64(0); i < n; i += step {
			left := uint16(funcs.floatBits(arr.ValueAt(i)) >> rightBW)
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

		exceptions := uint64(0)
		for left, count := range freq {
			inDict := false
			for i := 0; i < dictSize; i++ {
				if top[i].left == left {
					inDict = true
					break
				}
			}
			if !inDict {
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

// alprdArray stores ALP-RD encoded float values.
type alprdArray[T Float, J UnsignedInteger] struct {
	length        uint64
	rightBitWidth uint8
	dictSize      uint8
	dict          [alprdMaxDictSize]uint16
	leftParts     []byte // bit-packed left codes
	leftBitWidth  uint8
	rightParts    []byte // bit-packed right parts
	patches       *patches[uint16, J]
	floatFromBits func(uint64) T
}

// alprdBodySize returns the fixed body size for the header.
func alprdBodySize(dictSize uint8, leftBuf, rightBuf []byte) uint64 {
	return 1 + 1 + 1 + uint64(dictSize)*2 + 4 + uint64(len(leftBuf)) + 4 + uint64(len(rightBuf))
}

func (a *alprdArray[T, J]) Encoding() CodeType { return CodecTypeALPRD }
func (a *alprdArray[T, J]) Length() uint64     { return a.length }
func (a *alprdArray[T, J]) PType() PType       { return array.PTypeForType[T]() }
func (a *alprdArray[T, J]) BinarySize() uint64 {
	size := uint64(headerSize) + alprdBodySize(a.dictSize, a.leftParts, a.rightParts)
	if a.patches != nil {
		size += a.patches.BinarySize()
	}
	return size
}

func (a *alprdArray[T, J]) decode(offset uint64) T {
	code := unpackUnsigned(a.leftParts, offset*uint64(a.leftBitWidth), uint(a.leftBitWidth))
	left := uint64(a.dict[code])
	if leftOverride, ok := a.patches.ValueAt(offset); ok {
		left = uint64(leftOverride)
	}
	right := unpackUnsigned(a.rightParts, offset*uint64(a.rightBitWidth), uint(a.rightBitWidth))
	return a.floatFromBits((left << a.rightBitWidth) | right)
}

func (a *alprdArray[T, J]) ValueAt(offset uint64) T {
	if offset >= a.length {
		panic(errOffsetOutOfRange)
	}
	return a.decode(offset)
}

func (a *alprdArray[T, J]) DecompressInto(dst []T) error {
	if err := checkDstLen(dst, a.length); err != nil {
		return err
	}
	for i := uint64(0); i < a.length; i++ {
		code := unpackUnsigned(a.leftParts, i*uint64(a.leftBitWidth), uint(a.leftBitWidth))
		left := uint64(a.dict[code])
		right := unpackUnsigned(a.rightParts, i*uint64(a.rightBitWidth), uint(a.rightBitWidth))
		dst[i] = a.floatFromBits((left << a.rightBitWidth) | right)
	}
	if err := a.patches.Iterate(func(idx uint64, left uint16) {
		right := unpackUnsigned(a.rightParts, idx*uint64(a.rightBitWidth), uint(a.rightBitWidth))
		dst[idx] = a.floatFromBits((uint64(left) << a.rightBitWidth) | right)
	}); err != nil {
		return err
	}
	return nil
}

func (a *alprdArray[T, J]) Slice(start, end uint64) (EncodedArray[T], error) {
	if err := array.ValidateSliceBounds(a.length, start, end); err != nil {
		return nil, err
	}
	length := end - start
	sliced := &alprdArray[T, J]{
		length:        length,
		rightBitWidth: a.rightBitWidth,
		dictSize:      a.dictSize,
		dict:          a.dict,
		leftBitWidth:  a.leftBitWidth,
		floatFromBits: a.floatFromBits,
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

func (a *alprdArray[T, J]) WriteTo(w io.Writer) (int64, error) {
	flags := uint32(0)
	if a.patches != nil {
		flags |= flagALPRDHasPatches
	}
	bodySize := alprdBodySize(a.dictSize, a.leftParts, a.rightParts)
	n, err := codecHeader{
		Version:  versionNumber,
		Kind:     CodecTypeALPRD,
		ElemType: array.PTypeForType[T](),
		Flags:    flags,
		Length:   a.length,
		NumBytes: bodySize,
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
func buildALPRDArray[T Float](arr array.ArrayCore[T], ctx planContext) (EncodedArray[T], error) {
	if ctx.depth <= 0 {
		return nil, errDepthExhausted
	}
	if arr.Length() == 0 {
		return nil, errDataEmpty
	}

	var zero T
	switch any(zero).(type) {
	case float64:
		c, err := buildALPRDArrayTyped(any(arr).(array.ArrayCore[float64]), ctx, alprdFuncs64)
		if err != nil {
			return nil, err
		}
		return any(c).(EncodedArray[T]), nil
	case float32:
		c, err := buildALPRDArrayTyped(any(arr).(array.ArrayCore[float32]), ctx, alprdFuncs32)
		if err != nil {
			return nil, err
		}
		return any(c).(EncodedArray[T]), nil
	default:
		return nil, fmt.Errorf("codec: ALPRD not supported for %T", zero)
	}
}

func buildALPRDArrayTyped[T Float](arr array.ArrayCore[T], ctx planContext, funcs alprdFuncs[T]) (EncodedArray[T], error) {
	dict := alprdFindBestDict(arr, funcs)
	n := arr.Length()

	leftBuf := make([]byte, packedByteSize(n, uint(dict.leftBitWidth)))
	rightBuf := make([]byte, packedByteSize(n, uint(dict.rightBitWidth)))

	patchIdx := make([]uint64, 0)
	patchVals := make([]uint16, 0)

	for i := uint64(0); i < n; i++ {
		b := funcs.floatBits(arr.ValueAt(i))
		right := b & ((1 << dict.rightBitWidth) - 1)
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

	if len(patchIdx) == 0 {
		return &alprdArray[T, uint64]{
			length:        n,
			rightBitWidth: dict.rightBitWidth,
			dictSize:      dict.dictSize,
			dict:          dict.reverse,
			leftParts:     leftBuf,
			leftBitWidth:  dict.leftBitWidth,
			rightParts:    rightBuf,
			floatFromBits: funcs.floatFromBits,
		}, nil
	}

	var maxIdx uint64
	for _, v := range patchIdx {
		if v > maxIdx {
			maxIdx = v
		}
	}
	switch {
	case maxIdx <= uint64(^uint8(0)):
		return buildALPRDWithPatches[T, uint8](n, dict, leftBuf, rightBuf, funcs, patchIdx, patchVals, ctx)
	case maxIdx <= uint64(^uint16(0)):
		return buildALPRDWithPatches[T, uint16](n, dict, leftBuf, rightBuf, funcs, patchIdx, patchVals, ctx)
	case maxIdx <= uint64(^uint32(0)):
		return buildALPRDWithPatches[T, uint32](n, dict, leftBuf, rightBuf, funcs, patchIdx, patchVals, ctx)
	default:
		return buildALPRDWithPatches[T, uint64](n, dict, leftBuf, rightBuf, funcs, patchIdx, patchVals, ctx)
	}
}

func buildALPRDWithPatches[T Float, J UnsignedInteger](n uint64, dict alprdDict, leftBuf, rightBuf []byte, funcs alprdFuncs[T], patchIdx []uint64, patchVals []uint16, ctx planContext) (EncodedArray[T], error) {
	patches, err := buildALPRDPatchesTyped[J](n, patchIdx, patchVals, ctx)
	if err != nil {
		return nil, err
	}
	return &alprdArray[T, J]{
		length:        n,
		rightBitWidth: dict.rightBitWidth,
		dictSize:      dict.dictSize,
		dict:          dict.reverse,
		leftParts:     leftBuf,
		leftBitWidth:  dict.leftBitWidth,
		rightParts:    rightBuf,
		patches:       patches,
		floatFromBits: funcs.floatFromBits,
	}, nil
}

func buildALPRDPatchesTyped[J UnsignedInteger](length uint64, patchIdx []uint64, patchVals []uint16, ctx planContext) (*patches[uint16, J], error) {
	narrow := make([]J, len(patchIdx))
	for i, v := range patchIdx {
		narrow[i] = J(v)
	}
	patchIdxCodec, err := compressArray(array.NewPrimitivesUnsafe(narrow), ctx.descend().withIntegerExcludes(CodecTypeDict, CodecTypeRunEnd))
	if err != nil {
		return nil, err
	}
	if isAllSameUnsigned(patchVals) {
		patchValCodec, err := newConstIntegerArray(array.NewPrimitivesUnsafe(patchVals))
		if err != nil {
			return nil, err
		}
		return newPatches[uint16, J](length, 0, patchIdxCodec, patchValCodec)
	}
	return newPatches[uint16, J](length, 0, patchIdxCodec, newRawArray(array.NewPrimitivesUnsafe(patchVals)))
}

func estimateALPRD[T Float, S statsSource[T]](stats S, ctx planContext, isConst bool) (float64, bool) {
	if ctx.depth <= 0 || isConst {
		return 0, false
	}
	return estimateBySample(stats, ctx, buildALPRDArray[T])
}

// readAnyALPRDArray dispatches deserialization by float type.
func readAnyALPRDArray[T Integer | Float | String](br *array.BufReader, h codecHeader, opts ReadOptions) (EncodedArray[T], error) {
	var zero T
	switch any(zero).(type) {
	case float64:
		return readCast[T](readALPRDArrayTyped[float64](br, h, opts, alprdFuncs64.floatFromBits))
	case float32:
		return readCast[T](readALPRDArrayTyped[float32](br, h, opts, alprdFuncs32.floatFromBits))
	default:
		return nil, fmt.Errorf("codec: ALPRD not supported for %v", h.ElemType)
	}
}

func readALPRDArrayTyped[T Float](br *array.BufReader, h codecHeader, opts ReadOptions, floatFromBits func(uint64) T) (EncodedArray[T], error) {
	if h.Flags&^flagALPRDHasPatches != 0 {
		return nil, fmt.Errorf("codec: unsupported ALPRD flags = 0x%x", h.Flags)
	}
	// Plausibility bound: body holds 3 fixed bytes + up to 8 dict entries (16 bytes)
	// + two length-prefixed bit-packed buffers. Each buffer is at most
	// length * totalBits / 8 bytes. With totalBits <= 64 for float64 and two
	// buffers (left + right), the packed data is bounded by 2 * length * 8.
	maxBody := uint64(3+alprdMaxDictSize*2+8) + 2*h.Length*8
	if h.NumBytes > maxBody {
		return nil, fmt.Errorf("codec: ALPRD body size %d implausible for length %d", h.NumBytes, h.Length)
	}
	body, err := br.Read(int(h.NumBytes))
	if err != nil {
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
	leftParts := body[off : off+int(leftLen)]
	off += int(leftLen)

	rightLen := binary.LittleEndian.Uint32(body[off:])
	off += 4
	rightParts := body[off : off+int(rightLen)]

	if h.Flags&flagALPRDHasPatches == 0 {
		return &alprdArray[T, uint64]{
			length:        h.Length,
			rightBitWidth: rightBitWidth,
			dictSize:      dictSize,
			dict:          dict,
			leftParts:     leftParts,
			leftBitWidth:  leftBitWidth,
			rightParts:    rightParts,
			floatFromBits: floatFromBits,
		}, nil
	}

	data, err := br.Read(8)
	if err != nil {
		return nil, err
	}
	offset := binary.LittleEndian.Uint64(data)
	idxHeader, err := readHeader(br)
	if err != nil {
		return nil, err
	}

	switch idxHeader.ElemType {
	case PTypeUint8:
		return readALPRDWithPatchIdx[T, uint8](br, h, opts, rightBitWidth, leftBitWidth, dictSize, dict, leftParts, rightParts, floatFromBits, offset, idxHeader)
	case PTypeUint16:
		return readALPRDWithPatchIdx[T, uint16](br, h, opts, rightBitWidth, leftBitWidth, dictSize, dict, leftParts, rightParts, floatFromBits, offset, idxHeader)
	case PTypeUint32:
		return readALPRDWithPatchIdx[T, uint32](br, h, opts, rightBitWidth, leftBitWidth, dictSize, dict, leftParts, rightParts, floatFromBits, offset, idxHeader)
	case PTypeUint64:
		return readALPRDWithPatchIdx[T, uint64](br, h, opts, rightBitWidth, leftBitWidth, dictSize, dict, leftParts, rightParts, floatFromBits, offset, idxHeader)
	default:
		return nil, fmt.Errorf("codec: ALPRD patch index type = %v, want unsigned integer", idxHeader.ElemType)
	}
}

func readALPRDWithPatchIdx[T Float, J UnsignedInteger](br *array.BufReader, h codecHeader, opts ReadOptions, rightBitWidth, leftBitWidth, dictSize uint8, dict [alprdMaxDictSize]uint16, leftParts, rightParts []byte, floatFromBits func(uint64) T, offset uint64, idxHeader codecHeader) (EncodedArray[T], error) {
	idxCodec, err := readEncodedArrayWithHeader[J](br, idxHeader, opts)
	if err != nil {
		return nil, err
	}
	valCodec, err := readEncodedArray[uint16](br, opts)
	if err != nil {
		return nil, err
	}
	patches, err := newPatches[uint16, J](h.Length, offset, idxCodec, valCodec)
	if err != nil {
		return nil, prefixPatchError(err, "ALPRD")
	}
	return &alprdArray[T, J]{
		length:        h.Length,
		rightBitWidth: rightBitWidth,
		dictSize:      dictSize,
		dict:          dict,
		leftParts:     leftParts,
		leftBitWidth:  leftBitWidth,
		rightParts:    rightParts,
		patches:       patches,
		floatFromBits: floatFromBits,
	}, nil
}
