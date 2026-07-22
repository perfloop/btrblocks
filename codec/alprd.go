package codec

import (
	"cmp"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"math/bits"
	"slices"

	"github.com/axiomhq/btrblocks/array"
)

const (
	// alprdMaxDictSize limits the left-part dictionary to 8 entries (3-bit
	// codes). Larger dictionaries show diminishing compression gains in the
	// ALP-RD paper while increasing encoding overhead.
	alprdMaxDictSize = 8
	// alprdMaxCutBits is the maximum bit-width for the right (residual) part.
	// Beyond 16 bits the left part covers too few bits to compress
	// effectively via a small dictionary.
	alprdMaxCutBits            = 16
	flagALPRDHasPatches uint32 = 1 << 0
)

var errALPRDHighPatchRatio = ErrALPRDHighPatchRatio

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

func alprdFindBestDict[T Float](arr array.ArrayCore[T], funcs alprdFuncs[T], budget buildBudget) (alprdDict, error) {
	n := arr.Length()
	// Sample up to 1024 values for dictionary learning. Dictionary selection
	// only needs to identify the top-K frequent left-part prefixes, which
	// converges quickly on even moderately sized columns.
	step := uint64(1)
	if n > 1024 {
		step = n / 1024
	}

	// Materialize sampled bit-patterns once to avoid repeated interface
	// dispatch across ~alprdMaxCutBits passes.
	sampleCount := n / step
	if n%step != 0 {
		sampleCount++
	}
	sampleBits, err := makeBuildSlice[uint64](budget, 0, sampleCount, "ALP-RD dictionary samples")
	if err != nil {
		return alprdDict{}, err
	}
	for i := uint64(0); i < n; i += step {
		sampleBits = append(sampleBits, funcs.floatBits(arr.ValueAt(i)))
	}

	var best alprdDict
	bestCost := math.MaxFloat64
	freq := make(map[uint16]uint64)

	for cut := uint8(1); cut <= alprdMaxCutBits; cut++ {
		rightBW := funcs.totalBits - cut

		clear(freq)
		sampleN := uint64(0)
		for _, bits := range sampleBits {
			left := uint16(bits >> rightBW)
			if _, exists := freq[left]; !exists {
				if err := checkBuildMapEntries[uint16](budget, uint64(len(freq))+1, "ALP-RD frequency map"); err != nil {
					return alprdDict{}, err
				}
			}
			freq[left]++
			sampleN++
		}

		type entry struct {
			left  uint16
			count uint64
		}
		top, err := makeBuildSlice[entry](budget, 0, uint64(len(freq)), "ALP-RD frequency entries")
		if err != nil {
			return alprdDict{}, err
		}
		for left, count := range freq {
			top = append(top, entry{left, count})
		}
		slices.SortFunc(top, func(a, b entry) int {
			if byCount := cmp.Compare(b.count, a.count); byCount != 0 {
				return byCount
			}
			return cmp.Compare(a.left, b.left)
		})

		dictSize := min(len(top), alprdMaxDictSize)

		exceptions := uint64(0)
		for left, count := range freq {
			inDict := false
			for i := range dictSize {
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
			if err := checkBuildMapEntries[uint16](budget, uint64(dictSize), "ALP-RD dictionary map"); err != nil {
				return alprdDict{}, err
			}
			fwd := make(map[uint16]uint8, dictSize)
			var rev [alprdMaxDictSize]uint16
			for i := range dictSize {
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
	return best, nil
}

// alprdArray stores ALP-RD encoded float values.
type alprdArray[T Float, J UnsignedInteger] struct {
	encodedNode
	denseRows
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

func (a *alprdArray[T, J]) CodecType() CodecType {
	return CodecTypeALPRD
}
func (a *alprdArray[T, J]) PType() PType { return array.PTypeOfPrimitive[T]() }
func (a *alprdArray[T, J]) DecodedBytes() (uint64, error) {
	// Left and right parts are unpacked from the body straight into dst; only
	// the patch children add buffers.
	var f decodeFootprint
	f.add(decodedBytesFor(a.Length(), a.PType()))
	f.add(a.patches.decodedBytes())
	return f.result()
}
func (a *alprdArray[T, J]) BinarySize() uint64 {
	size := uint64(headerSize) + alprdBodySize(a.dictSize, a.leftParts, a.rightParts)
	if a.patches != nil {
		size += a.patches.BinarySize()
	}
	return size
}

func (a *alprdArray[T, J]) MarshalBinary() ([]byte, error) { return marshalBinary(a) }

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
	if offset >= a.Length() {
		panic(errOffsetOutOfRange)
	}
	return a.decode(offset)
}

func (a *alprdArray[T, J]) DecompressInto(dst []T) error {
	if err := checkDstLen(dst, a.Length()); err != nil {
		return err
	}
	for i := range a.Length() {
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
	if err := array.ValidateSliceBounds(a.Length(), start, end); err != nil {
		return nil, err
	}
	if start == end {
		return slicePrimitiveToRawArray(a, start, end)
	}
	length := end - start
	sliced := &alprdArray[T, J]{
		denseRows:     denseRows(length),
		rightBitWidth: a.rightBitWidth,
		dictSize:      a.dictSize,
		dict:          a.dict,
		leftBitWidth:  a.leftBitWidth,
		floatFromBits: a.floatFromBits,
	}
	if length > 0 {
		sliced.leftParts = make([]byte, packedByteSize(length, uint(a.leftBitWidth)))
		for i := range length {
			v := unpackUnsigned(a.leftParts, (start+i)*uint64(a.leftBitWidth), uint(a.leftBitWidth))
			packUnsigned(sliced.leftParts, i*uint64(a.leftBitWidth), uint(a.leftBitWidth), v)
		}
		sliced.rightParts = make([]byte, packedByteSize(length, uint(a.rightBitWidth)))
		for i := range length {
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
	sum := writeSum{w: w}
	if err := sum.writeTo(codecHeader{
		Version:  versionNumber,
		Type:     CodecTypeALPRD,
		ElemType: array.PTypeOfPrimitive[T](),
		Flags:    flags,
		Length:   a.Length(),
		NumBytes: bodySize,
	}); err != nil {
		return sum.n, err
	}

	buf := make([]byte, bodySize)
	off := 0
	buf[off] = a.rightBitWidth
	off++
	buf[off] = a.leftBitWidth
	off++
	buf[off] = a.dictSize
	off++
	for i := range a.dictSize {
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

	if err := sum.write(buf); err != nil {
		return sum.n, err
	}

	if a.patches != nil {
		if err := sum.writeTo(a.patches); err != nil {
			return sum.n, err
		}
	}
	return sum.n, nil
}

// encodeALPRD32 transforms arr and leaves patch-index compression to the caller.
func encodeALPRD32(arr array.ArrayCore[float32], children UnsignedChildBuilder, budget buildBudget) (EncodedArray[float32], error) {
	if arr.Length() == 0 {
		return nil, errDataEmpty
	}
	if children == nil {
		return nil, ErrBuilderRequired
	}
	return buildALPRDArrayTyped(arr, alprdFuncs32, children, budget)
}

// encodeALPRD64 transforms arr and leaves patch-index compression to the caller.
func encodeALPRD64(arr array.ArrayCore[float64], children UnsignedChildBuilder, budget buildBudget) (EncodedArray[float64], error) {
	if arr.Length() == 0 {
		return nil, errDataEmpty
	}
	if children == nil {
		return nil, ErrBuilderRequired
	}
	return buildALPRDArrayTyped(arr, alprdFuncs64, children, budget)
}

func buildALPRDArrayTyped[T Float](arr array.ArrayCore[T], funcs alprdFuncs[T], children UnsignedChildBuilder, budget buildBudget) (EncodedArray[T], error) {
	n := arr.Length()

	// Materialize once to avoid per-element interface dispatch across
	// dict search and encoding.
	vals, err := makeBuildSlice[T](budget, n, n, "ALP-RD source values")
	if err != nil {
		return nil, err
	}
	for i := range n {
		vals[i] = arr.ValueAt(i)
	}

	dict, err := alprdFindBestDict(array.NewPrimitivesUnsafe(vals), funcs, budget)
	if err != nil {
		return nil, err
	}

	leftSize, err := checkedPackedByteSize(n, uint(dict.leftBitWidth))
	if err != nil {
		return nil, err
	}
	leftBuf, err := makeBuildSlice[byte](budget, uint64(leftSize), uint64(leftSize), "ALP-RD left parts")
	if err != nil {
		return nil, err
	}
	rightSize, err := checkedPackedByteSize(n, uint(dict.rightBitWidth))
	if err != nil {
		return nil, err
	}
	rightBuf, err := makeBuildSlice[byte](budget, uint64(rightSize), uint64(rightSize), "ALP-RD right parts")
	if err != nil {
		return nil, err
	}

	patchIdx := make([]uint64, 0)
	patchVals := make([]uint16, 0)

	for i := range n {
		b := funcs.floatBits(vals[i])
		right := b & ((1 << dict.rightBitWidth) - 1)
		left := uint16(b >> dict.rightBitWidth)

		code, ok := dict.forward[left]
		if !ok {
			if uint64(len(patchIdx))+1 > n/2 {
				return nil, errALPRDHighPatchRatio
			}
			patchIdx, err = appendBuildValue(patchIdx, i, budget, "ALP-RD patch indices")
			if err != nil {
				return nil, err
			}
			patchVals, err = appendBuildValue(patchVals, left, budget, "ALP-RD patch values")
			if err != nil {
				return nil, err
			}
			code = 0
		}

		packUnsigned(leftBuf, i*uint64(dict.leftBitWidth), uint(dict.leftBitWidth), uint64(code))
		packUnsigned(rightBuf, i*uint64(dict.rightBitWidth), uint(dict.rightBitWidth), right)
	}

	if len(patchIdx) == 0 {
		return &alprdArray[T, uint64]{
			denseRows:     denseRows(n),
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
		return buildALPRDWithPatches(n, dict, leftBuf, rightBuf, funcs, patchIdx, patchVals, children.BuildUint8, budget)
	case maxIdx <= uint64(^uint16(0)):
		return buildALPRDWithPatches(n, dict, leftBuf, rightBuf, funcs, patchIdx, patchVals, children.BuildUint16, budget)
	case maxIdx <= uint64(^uint32(0)):
		return buildALPRDWithPatches(n, dict, leftBuf, rightBuf, funcs, patchIdx, patchVals, children.BuildUint32, budget)
	default:
		return buildALPRDWithPatches(n, dict, leftBuf, rightBuf, funcs, patchIdx, patchVals, children.BuildUint64, budget)
	}
}

func buildALPRDWithPatches[T Float, J UnsignedInteger](n uint64, dict alprdDict, leftBuf, rightBuf []byte, funcs alprdFuncs[T], patchIdx []uint64, patchVals []uint16, buildIndices ChildBuilder[J], budget buildBudget) (EncodedArray[T], error) {
	patches, err := buildALPRDPatchesTyped(n, patchIdx, patchVals, buildIndices, budget)
	if err != nil {
		return nil, err
	}
	return &alprdArray[T, J]{
		denseRows:     denseRows(n),
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

func buildALPRDPatchesTyped[J UnsignedInteger](length uint64, patchIdx []uint64, patchVals []uint16, buildIndices ChildBuilder[J], budget buildBudget) (*patches[uint16, J], error) {
	narrow, err := makeBuildSlice[J](budget, uint64(len(patchIdx)), uint64(len(patchIdx)), "ALP-RD narrowed patch indices")
	if err != nil {
		return nil, err
	}
	for i, v := range patchIdx {
		narrow[i] = J(v)
	}
	patchIdxCodec, err := buildIndices(array.NewPrimitivesUnsafe(narrow))
	patchIdxCodec, err = adoptChild(uint64(len(narrow)), patchIdxCodec, err)
	if err != nil {
		return nil, fmt.Errorf("codec: compress ALP-RD patch indices: %w", err)
	}
	if isAllSameUnsigned(patchVals) {
		patchValCodec, err := newConstIntegerArray(array.NewPrimitivesUnsafe(patchVals))
		if err != nil {
			return nil, err
		}
		return newPatches(length, 0, patchIdxCodec, patchValCodec, narrow)
	}
	return newPatches(length, 0, patchIdxCodec, newRawArray(array.NewPrimitivesUnsafe(patchVals)), narrow)
}

func readALPRDArrayTyped[T Float](br *array.BufReader, h codecHeader, opts *readOptions, funcs alprdFuncs[T]) (EncodedArray[T], error) {
	floatFromBits := funcs.floatFromBits
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
		return nil, fmt.Errorf("codec: reading ALPRD body: %w", err)
	}

	// Minimum body: 3 fixed bytes (rightBW, leftBW, dictSize).
	if len(body) < 3 {
		return nil, fmt.Errorf("codec: ALPRD body too small (%d bytes)", len(body))
	}
	off := 0
	rightBitWidth := body[off]
	off++
	leftBitWidth := body[off]
	off++
	dictSize := body[off]
	off++

	if dictSize > alprdMaxDictSize {
		return nil, fmt.Errorf("codec: ALPRD dict size = %d, max %d", dictSize, alprdMaxDictSize)
	}
	if h.Length == 0 {
		return nil, fmt.Errorf("codec: ALPRD length = 0")
	}
	// decode indexes the fixed [alprdMaxDictSize]uint16 dictionary with a
	// leftBitWidth-bit code; the build path never emits widths above
	// bits.Len(alprdMaxDictSize-1). Reject wider codes so decode can never
	// panic on crafted input, and bound the right part to the float width.
	if maxLeftBits := uint8(bits.Len(alprdMaxDictSize - 1)); leftBitWidth > maxLeftBits {
		return nil, fmt.Errorf("codec: ALPRD left bit width = %d, max %d", leftBitWidth, maxLeftBits)
	}
	if rightBitWidth == 0 || rightBitWidth >= funcs.totalBits {
		return nil, fmt.Errorf("codec: ALPRD right bit width = %d, max %d", rightBitWidth, funcs.totalBits)
	}
	if leftBitWidth == 0 || dictSize == 0 {
		return nil, fmt.Errorf("codec: ALPRD left bit width = %d, dict size = %d", leftBitWidth, dictSize)
	}

	// Need dictSize*2 bytes for dict entries + 4 bytes for leftLen + 4 bytes for rightLen.
	minRemaining := int(dictSize)*2 + 4 + 4
	if len(body)-off < minRemaining {
		return nil, fmt.Errorf("codec: ALPRD body too small for dict + buffer lengths")
	}

	var dict [alprdMaxDictSize]uint16
	for i := range dictSize {
		dict[i] = binary.LittleEndian.Uint16(body[off:])
		off += 2
	}

	leftLen := binary.LittleEndian.Uint32(body[off:])
	off += 4
	if len(body)-off < int(leftLen) {
		return nil, fmt.Errorf("codec: ALPRD left buffer length %d exceeds remaining body", leftLen)
	}
	if want, err := checkedPackedByteSize(h.Length, uint(leftBitWidth)); err != nil || int(leftLen) != want {
		return nil, fmt.Errorf("codec: ALPRD left buffer length = %d, want %d", leftLen, want)
	}
	leftParts := body[off : off+int(leftLen)]
	off += int(leftLen)

	if len(body)-off < 4 {
		return nil, fmt.Errorf("codec: ALPRD body too small for right buffer length")
	}
	rightLen := binary.LittleEndian.Uint32(body[off:])
	off += 4
	if len(body)-off < int(rightLen) {
		return nil, fmt.Errorf("codec: ALPRD right buffer length %d exceeds remaining body", rightLen)
	}
	if want, err := checkedPackedByteSize(h.Length, uint(rightBitWidth)); err != nil || int(rightLen) != want {
		return nil, fmt.Errorf("codec: ALPRD right buffer length = %d, want %d", rightLen, want)
	}
	rightParts := body[off : off+int(rightLen)]
	if off+int(rightLen) != len(body) {
		return nil, fmt.Errorf("codec: ALPRD body has %d trailing bytes", len(body)-(off+int(rightLen)))
	}
	for i := range h.Length {
		if code := unpackUnsigned(leftParts, i*uint64(leftBitWidth), uint(leftBitWidth)); code >= uint64(dictSize) {
			return nil, fmt.Errorf("codec: ALPRD dictionary code = %d at position %d, want < %d", code, i, dictSize)
		}
	}

	if h.Flags&flagALPRDHasPatches == 0 {
		return &alprdArray[T, uint64]{
			denseRows:     denseRows(h.Length),
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
		return nil, fmt.Errorf("codec: reading ALPRD patch offset: %w", err)
	}
	offset := binary.LittleEndian.Uint64(data)
	idxHeader, err := readHeader(br)
	if err != nil {
		return nil, fmt.Errorf("codec: reading ALPRD patch indices header: %w", err)
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

func readALPRDWithPatchIdx[T Float, J UnsignedInteger](br *array.BufReader, h codecHeader, opts *readOptions, rightBitWidth, leftBitWidth, dictSize uint8, dict [alprdMaxDictSize]uint16, leftParts, rightParts []byte, floatFromBits func(uint64) T, offset uint64, idxHeader codecHeader) (EncodedArray[T], error) {
	idxCodec, err := readUnsignedEncodedArrayWithHeader[J](br, idxHeader, opts)
	if err != nil {
		return nil, fmt.Errorf("codec: reading ALPRD patch indices: %w", err)
	}
	if err := requireNonNullable(idxCodec, "ALPRD patch indices"); err != nil {
		return nil, err
	}
	valCodec, err := readUnsignedEncodedArray[uint16](br, opts)
	if err != nil {
		return nil, fmt.Errorf("codec: reading ALPRD patch values: %w", err)
	}
	if err := requireNonNullable(valCodec, "ALPRD patch values"); err != nil {
		return nil, err
	}
	patches, err := readPatches(h.Length, offset, idxCodec, valCodec, opts)
	if err != nil {
		return nil, prefixPatchError(err, "ALPRD")
	}
	return &alprdArray[T, J]{
		denseRows:     denseRows(h.Length),
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
