package codec

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"unsafe"

	"github.com/axiomhq/btrblocks/array"
)

// Power-of-10 lookup tables for ALP encode/decode. Literal values avoid init().
var pow10F64 = [24]float64{
	1, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9, 1e10, 1e11,
	1e12, 1e13, 1e14, 1e15, 1e16, 1e17, 1e18, 1e19, 1e20, 1e21, 1e22, 1e23,
}
var ipow10F64 = [24]float64{
	1, 1e-1, 1e-2, 1e-3, 1e-4, 1e-5, 1e-6, 1e-7, 1e-8, 1e-9, 1e-10, 1e-11,
	1e-12, 1e-13, 1e-14, 1e-15, 1e-16, 1e-17, 1e-18, 1e-19, 1e-20, 1e-21, 1e-22, 1e-23,
}
var pow10F32 = [11]float32{
	1, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9, 1e10,
}
var ipow10F32 = [11]float32{
	1, 1e-1, 1e-2, 1e-3, 1e-4, 1e-5, 1e-6, 1e-7, 1e-8, 1e-9, 1e-10,
}

const flagALPHasPatches uint32 = 1 << 0

var errALPHighPatchRatio = ErrALPHighPatchRatio

func alpEncode64(value float64, e, f uint8) int64 {
	return int64(math.Round(value * pow10F64[e] * ipow10F64[f]))
}

func alpDecode64(encoded int64, e, f uint8) float64 {
	return float64(encoded) * ipow10F64[e] * pow10F64[f]
}

func alpIsException64(value float64, e, f uint8) bool {
	return math.Float64bits(alpDecode64(alpEncode64(value, e, f), e, f)) != math.Float64bits(value)
}

func alpEncode32(value float32, e, f uint8) int32 {
	return int32(math.Round(float64(value) * float64(pow10F32[e]) * float64(ipow10F32[f])))
}

func alpDecode32(encoded int32, e, f uint8) float32 {
	return float32(encoded) * ipow10F32[e] * pow10F32[f]
}

func alpIsException32(value float32, e, f uint8) bool {
	return math.Float32bits(alpDecode32(alpEncode32(value, e, f), e, f)) != math.Float32bits(value)
}

func findBestExponents[T Float](arr array.ArrayCore[T], maxE uint8, isException func(T, uint8, uint8) bool) (uint8, uint8) {
	n := arr.Length()
	// Sample up to 64 values for exponent selection. The optimal (e,f) pair
	// depends on the data's decimal structure, which is typically uniform
	// across the column, so a small sample suffices.
	step := uint64(1)
	if n > 64 {
		step = n / 64
	}

	// Materialize sampled values once to avoid repeated interface dispatch
	// across ~maxE² (e,f) passes.
	samples := make([]T, 0, (n+step-1)/step)
	for i := uint64(0); i < n; i += step {
		samples = append(samples, arr.ValueAt(i))
	}

	var bestE, bestF uint8
	bestExceptions := uint64(math.MaxUint64)
	for e := range maxE {
		for f := range e {
			exceptions := uint64(0)
			for _, v := range samples {
				if isException(v, e, f) {
					exceptions++
				}
			}
			if exceptions < bestExceptions || (exceptions == bestExceptions && (e-f) < (bestE-bestF)) {
				bestExceptions = exceptions
				bestE = e
				bestF = f
			}
		}
	}
	return bestE, bestF
}

// alpFuncs bundles width-specific ALP operations for float32 or float64.
type alpFuncs[T Float, I SignedInteger] struct {
	encode      func(T, uint8, uint8) I
	decode      func(I, uint8, uint8) T
	isException func(T, uint8, uint8) bool
	maxE        uint8
	toBits      func(T) uint64
}

var alpFuncs64 = alpFuncs[float64, int64]{
	encode:      alpEncode64,
	decode:      alpDecode64,
	isException: alpIsException64,
	maxE:        23, // largest power-of-10 representable exactly in float64 (52-bit mantissa)
	toBits:      math.Float64bits,
}

var alpFuncs32 = alpFuncs[float32, int32]{
	encode:      alpEncode32,
	decode:      alpDecode32,
	isException: alpIsException32,
	maxE:        10, // largest power-of-10 representable exactly in float32 (23-bit mantissa)
	toBits:      func(v float32) uint64 { return uint64(math.Float32bits(v)) },
}

func isAllSameFloat[T Float](values []T, toBits func(T) uint64) bool {
	if len(values) == 0 {
		return true
	}
	bits0 := toBits(values[0])
	for _, value := range values[1:] {
		if toBits(value) != bits0 {
			return false
		}
	}
	return true
}

// alpArray stores an ALP-encoded integer child plus optional float exceptions.
type alpArray[T Float, I SignedInteger, J UnsignedInteger] struct {
	encodedNode
	denseRows
	expE    uint8
	expF    uint8
	encoded EncodedArray[I]
	patches *patches[T, J]
	decode  func(I, uint8, uint8) T
}

const alpBodySize = 2

func (a *alpArray[T, I, J]) CodecType() CodecType {
	return CodecTypeALP
}
func (a *alpArray[T, I, J]) PType() PType { return array.PTypeOfPrimitive[T]() }
func (a *alpArray[T, I, J]) DecodedBytes() (uint64, error) {
	// The integer child decodes into dst's backing memory — same width, same
	// element count — so its footprint already includes this node's
	// destination; only the patch children add buffers.
	var f decodeFootprint
	f.add(a.encoded.DecodedBytes())
	f.add(a.patches.decodedBytes())
	return f.result()
}
func (a *alpArray[T, I, J]) BinarySize() uint64 {
	size := uint64(headerSize) + alpBodySize + a.encoded.BinarySize()
	if a.patches != nil {
		size += a.patches.BinarySize()
	}
	return size
}

func (a *alpArray[T, I, J]) ValueAt(offset uint64) T {
	if offset >= a.Length() {
		panic(errOffsetOutOfRange)
	}
	if value, ok := a.patches.ValueAt(offset); ok {
		return value
	}
	return a.decode(a.encoded.ValueAt(offset), a.expE, a.expF)
}

// DecompressInto decodes the ALP integer child directly into dst's backing
// memory, then replaces each integer with its decoded float in the same slot.
// ALP only pairs float32 with int32 and float64 with int64, so the slices have
// identical widths and contain no pointers. Each integer is read before its
// destination slot is overwritten. This keeps bulk child decoding without an
// N*int_width intermediate allocation.
func (a *alpArray[T, I, J]) DecompressInto(dst []T) error {
	if err := checkDstLen(dst, a.Length()); err != nil {
		return err
	}
	if unsafe.Sizeof(T(0)) != unsafe.Sizeof(I(0)) {
		return fmt.Errorf("codec: ALP float width %d differs from integer width %d", unsafe.Sizeof(T(0)), unsafe.Sizeof(I(0)))
	}
	encoded := unsafe.Slice((*I)(unsafe.Pointer(unsafe.SliceData(dst))), int(a.Length()))
	if err := a.encoded.DecompressInto(encoded); err != nil {
		return fmt.Errorf("codec: decompress ALP integer child: %w", err)
	}
	for i, value := range encoded {
		dst[i] = a.decode(value, a.expE, a.expF)
	}
	if err := a.patches.Apply(dst[:a.Length()]); err != nil {
		return fmt.Errorf("codec: apply ALP patches: %w", err)
	}
	return nil
}

func (a *alpArray[T, I, J]) Slice(start, end uint64) (EncodedArray[T], error) {
	if err := array.ValidateSliceBounds(a.Length(), start, end); err != nil {
		return nil, err
	}
	encoded, err := a.encoded.Slice(start, end)
	if err != nil {
		return nil, err
	}
	patches, err := a.patches.Slice(start, end)
	if err != nil {
		return nil, err
	}
	return &alpArray[T, I, J]{denseRows: denseRows(end - start), expE: a.expE, expF: a.expF, encoded: encoded, patches: patches, decode: a.decode}, nil
}

func (a *alpArray[T, I, J]) WriteTo(w io.Writer) (int64, error) {
	flags := uint32(0)
	if a.patches != nil {
		flags |= flagALPHasPatches
	}
	sum := writeSum{w: w}
	if err := sum.writeTo(codecHeader{
		Version:  versionNumber,
		Type:     CodecTypeALP,
		ElemType: array.PTypeOfPrimitive[T](),
		Flags:    flags,
		Length:   a.Length(),
		NumBytes: alpBodySize,
	}); err != nil {
		return sum.n, err
	}

	var buf [2]byte
	buf[0] = a.expE
	buf[1] = a.expF
	if err := sum.write(buf[:2]); err != nil {
		return sum.n, err
	}

	if err := sum.writeTo(a.encoded); err != nil {
		return sum.n, err
	}
	if a.patches != nil {
		if err := sum.writeTo(a.patches); err != nil {
			return sum.n, err
		}
	}
	return sum.n, nil
}

func readALPArrayTyped[T Float, I SignedInteger](br *array.BufReader, h codecHeader, opts *readOptions, funcs alpFuncs[T, I], readValues encodedReader[T]) (EncodedArray[T], error) {
	if h.Flags&^flagALPHasPatches != 0 {
		return nil, fmt.Errorf("codec: unsupported ALP flags = 0x%x", h.Flags)
	}
	if h.NumBytes != alpBodySize {
		return nil, fmt.Errorf("codec: ALP body size = %d, want %d", h.NumBytes, alpBodySize)
	}
	data, err := br.Read(2)
	if err != nil {
		return nil, fmt.Errorf("codec: reading ALP exponents: %w", err)
	}
	var buf [2]byte
	buf[0] = data[0]
	buf[1] = data[1]
	// The decode functions index fixed pow10 tables of maxE+1 entries; reject
	// out-of-range exponents here so decode can never panic on crafted input.
	if buf[0] > funcs.maxE || buf[1] > funcs.maxE {
		return nil, fmt.Errorf("codec: ALP exponents e=%d f=%d exceed max %d", buf[0], buf[1], funcs.maxE)
	}
	decode := funcs.decode
	encoded, err := readSignedEncodedArray[I](br, opts)
	if err != nil {
		return nil, fmt.Errorf("codec: reading ALP encoded child: %w", err)
	}
	if err := requireNonNullable(encoded, "ALP encoded values"); err != nil {
		return nil, err
	}
	if encoded.Length() != h.Length {
		return nil, fmt.Errorf("codec: ALP length = %d, want %d", encoded.Length(), h.Length)
	}

	if h.Flags&flagALPHasPatches == 0 {
		return &alpArray[T, I, uint64]{
			denseRows: denseRows(h.Length),
			expE:      buf[0],
			expF:      buf[1],
			encoded:   encoded,
			decode:    decode,
		}, nil
	}

	data, err = br.Read(8)
	if err != nil {
		return nil, fmt.Errorf("codec: reading ALP patch offset: %w", err)
	}
	offset := binary.LittleEndian.Uint64(data)
	idxHeader, err := readHeader(br)
	if err != nil {
		return nil, fmt.Errorf("codec: reading ALP patch indices header: %w", err)
	}
	switch idxHeader.ElemType {
	case PTypeUint8:
		return readALPWithPatchIdx[T, I, uint8](br, h, opts, buf, encoded, decode, offset, idxHeader, readValues)
	case PTypeUint16:
		return readALPWithPatchIdx[T, I, uint16](br, h, opts, buf, encoded, decode, offset, idxHeader, readValues)
	case PTypeUint32:
		return readALPWithPatchIdx[T, I, uint32](br, h, opts, buf, encoded, decode, offset, idxHeader, readValues)
	case PTypeUint64:
		return readALPWithPatchIdx[T, I, uint64](br, h, opts, buf, encoded, decode, offset, idxHeader, readValues)
	default:
		return nil, fmt.Errorf("codec: ALP patch index type = %v, want unsigned integer", idxHeader.ElemType)
	}
}

func readALPWithPatchIdx[T Float, I SignedInteger, J UnsignedInteger](br *array.BufReader, h codecHeader, opts *readOptions, buf [2]byte, encoded EncodedArray[I], decode func(I, uint8, uint8) T, offset uint64, idxHeader codecHeader, readValues encodedReader[T]) (EncodedArray[T], error) {
	idxCodec, err := readUnsignedEncodedArrayWithHeader[J](br, idxHeader, opts)
	if err != nil {
		return nil, fmt.Errorf("codec: reading ALP patch indices: %w", err)
	}
	if err := requireNonNullable(idxCodec, "ALP patch indices"); err != nil {
		return nil, err
	}
	valCodec, err := readValues(br, opts)
	if err != nil {
		return nil, fmt.Errorf("codec: reading ALP patch values: %w", err)
	}
	if err := requireNonNullable(valCodec, "ALP patch values"); err != nil {
		return nil, err
	}
	patches, err := readPatches(h.Length, offset, idxCodec, valCodec, opts)
	if err != nil {
		return nil, prefixPatchError(err, "ALP")
	}
	return &alpArray[T, I, J]{
		denseRows: denseRows(h.Length),
		expE:      buf[0],
		expF:      buf[1],
		encoded:   encoded,
		patches:   patches,
		decode:    decode,
	}, nil
}

// encodeALP32 transforms arr and delegates its encoded values and patch indices.
func encodeALP32(arr array.ArrayCore[float32], children ALPChildBuilder, budget buildBudget) (EncodedArray[float32], error) {
	if arr.Length() == 0 {
		return nil, errDataEmpty
	}
	if children == nil {
		return nil, ErrBuilderRequired
	}
	return buildALPArrayTyped(arr, alpFuncs32, children.BuildInt32, children, budget)
}

// encodeALP64 transforms arr and delegates its encoded values and patch indices.
func encodeALP64(arr array.ArrayCore[float64], children ALPChildBuilder, budget buildBudget) (EncodedArray[float64], error) {
	if arr.Length() == 0 {
		return nil, errDataEmpty
	}
	if children == nil {
		return nil, ErrBuilderRequired
	}
	return buildALPArrayTyped(arr, alpFuncs64, children.BuildInt64, children, budget)
}

func buildALPArrayTyped[T Float, I SignedInteger](arr array.ArrayCore[T], funcs alpFuncs[T, I], buildEncoded ChildBuilder[I], children ALPChildBuilder, budget buildBudget) (EncodedArray[T], error) {
	n := arr.Length()

	// Materialize once to avoid per-element interface dispatch across
	// exponent search, exception detection, and downstream encoding.
	vals, err := makeBuildSlice[T](budget, n, n, "ALP source values")
	if err != nil {
		return nil, err
	}
	for i := range n {
		vals[i] = arr.ValueAt(i)
	}

	e, f := findBestExponents(array.NewPrimitivesUnsafe(vals), funcs.maxE, funcs.isException)

	patchIdx := make([]uint64, 0)
	patchVals := make([]T, 0)
	for i, value := range vals {
		if funcs.isException(value, e, f) {
			if uint64(len(patchIdx))+1 > n/2 {
				return nil, errALPHighPatchRatio
			}
			patchIdx, err = appendBuildValue(patchIdx, uint64(i), budget, "ALP patch indices")
			if err != nil {
				return nil, err
			}
			patchVals, err = appendBuildValue(patchVals, value, budget, "ALP patch values")
			if err != nil {
				return nil, err
			}
		}
	}

	child, err := buildEncoded(array.NewVirtual(n, func(i uint64) I { return funcs.encode(vals[i], e, f) }))
	child, err = adoptChild(n, child, err)
	if err != nil {
		return nil, fmt.Errorf("codec: compress ALP encoded values: %w", err)
	}

	if len(patchIdx) == 0 {
		return &alpArray[T, I, uint64]{denseRows: denseRows(n), expE: e, expF: f, encoded: child, decode: funcs.decode}, nil
	}

	// Pick patch index width from max position value.
	var maxIdx uint64
	for _, v := range patchIdx {
		if v > maxIdx {
			maxIdx = v
		}
	}
	switch {
	case maxIdx <= uint64(^uint8(0)):
		return buildALPWithPatches(n, e, f, child, funcs, patchIdx, patchVals, children.BuildUint8, budget)
	case maxIdx <= uint64(^uint16(0)):
		return buildALPWithPatches(n, e, f, child, funcs, patchIdx, patchVals, children.BuildUint16, budget)
	case maxIdx <= uint64(^uint32(0)):
		return buildALPWithPatches(n, e, f, child, funcs, patchIdx, patchVals, children.BuildUint32, budget)
	default:
		return buildALPWithPatches(n, e, f, child, funcs, patchIdx, patchVals, children.BuildUint64, budget)
	}
}

func buildALPWithPatches[T Float, I SignedInteger, J UnsignedInteger](n uint64, e, f uint8, child EncodedArray[I], funcs alpFuncs[T, I], patchIdx []uint64, patchVals []T, buildIndices ChildBuilder[J], budget buildBudget) (EncodedArray[T], error) {
	patches, err := buildALPPatchesTyped(n, patchIdx, patchVals, buildIndices, funcs.toBits, budget)
	if err != nil {
		return nil, err
	}
	return &alpArray[T, I, J]{denseRows: denseRows(n), expE: e, expF: f, encoded: child, patches: patches, decode: funcs.decode}, nil
}

func buildALPPatchesTyped[T Float, J UnsignedInteger](length uint64, patchIdx []uint64, patchVals []T, buildIndices ChildBuilder[J], toBits func(T) uint64, budget buildBudget) (*patches[T, J], error) {
	narrow, err := makeBuildSlice[J](budget, uint64(len(patchIdx)), uint64(len(patchIdx)), "ALP narrowed patch indices")
	if err != nil {
		return nil, err
	}
	for i, v := range patchIdx {
		narrow[i] = J(v)
	}
	patchIdxCodec, err := buildIndices(array.NewPrimitivesUnsafe(narrow))
	patchIdxCodec, err = adoptChild(uint64(len(narrow)), patchIdxCodec, err)
	if err != nil {
		return nil, fmt.Errorf("codec: compress ALP patch indices: %w", err)
	}
	if isAllSameFloat(patchVals, toBits) {
		patchValCodec, err := newConstFloatArray(array.NewPrimitivesUnsafe(patchVals))
		if err != nil {
			return nil, err
		}
		return newPatches(length, 0, patchIdxCodec, patchValCodec, narrow)
	}
	return newPatches(length, 0, patchIdxCodec, newRawArray(array.NewPrimitivesUnsafe(patchVals)), narrow)
}
