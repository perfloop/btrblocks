package btrblocks

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"unsafe"

	"github.com/axiomhq/btrblocks/array"
)

var (
	pow10F64  [24]float64
	ipow10F64 [24]float64
	pow10F32  [11]float32
	ipow10F32 [11]float32
)

func init() {
	for i := range pow10F64 {
		pow10F64[i] = math.Pow(10, float64(i))
		ipow10F64[i] = math.Pow(10, -float64(i))
	}
	for i := range pow10F32 {
		pow10F32[i] = float32(math.Pow(10, float64(i)))
		ipow10F32[i] = float32(math.Pow(10, -float64(i)))
	}
}

const flagALPHasPatches uint32 = 1 << 0

var errALPHighPatchRatio = fmt.Errorf("codec: ALP patch ratio exceeds 50%%")

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

func findBestExponents64(arr array.ArrayCore[float64]) (uint8, uint8) {
	n := arr.Length()
	step := uint64(1)
	if n > 64 {
		step = n / 64
	}

	var bestE, bestF uint8
	bestExceptions := uint64(math.MaxUint64)
	for e := uint8(0); e < 23; e++ {
		for f := uint8(0); f < e; f++ {
			exceptions := uint64(0)
			for i := uint64(0); i < n; i += step {
				if alpIsException64(arr.ValueAt(i), e, f) {
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

func findBestExponents32(arr array.ArrayCore[float32]) (uint8, uint8) {
	n := arr.Length()
	step := uint64(1)
	if n > 64 {
		step = n / 64
	}

	var bestE, bestF uint8
	bestExceptions := uint64(math.MaxUint64)
	for e := uint8(0); e < 10; e++ {
		for f := uint8(0); f < e; f++ {
			exceptions := uint64(0)
			for i := uint64(0); i < n; i += step {
				if alpIsException32(arr.ValueAt(i), e, f) {
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
	findBest    func(array.ArrayCore[T]) (uint8, uint8)
	toBits      func(T) uint64
}

var alpFuncs64 = alpFuncs[float64, int64]{
	encode:      alpEncode64,
	decode:      alpDecode64,
	isException: alpIsException64,
	findBest:    findBestExponents64,
	toBits:      math.Float64bits,
}

var alpFuncs32 = alpFuncs[float32, int32]{
	encode:      alpEncode32,
	decode:      alpDecode32,
	isException: alpIsException32,
	findBest:    findBestExponents32,
	toBits:      func(v float32) uint64 { return uint64(math.Float32bits(v)) },
}

// alpEncodedArray exposes ALP-encoded float values as an integer leaf array.
// It implements array.Array[I] so it can be fed to compressArray.
type alpEncodedArray[T Float, I SignedInteger] struct {
	length  uint64
	expE    uint8
	expF    uint8
	valueAt func(uint64) T
	encode  func(T, uint8, uint8) I
}

func (a alpEncodedArray[T, I]) ValueAt(offset uint64) I {
	return a.encode(a.valueAt(offset), a.expE, a.expF)
}

func (a alpEncodedArray[T, I]) CopyTo(dst []I) {
	for i := range dst {
		dst[i] = a.encode(a.valueAt(uint64(i)), a.expE, a.expF)
	}
}

func (a alpEncodedArray[T, I]) BinarySize() uint64 {
	return array.HeaderSize + a.length*uint64(array.PTypeForType[I]().ByteWidth())
}

func (a alpEncodedArray[T, I]) Length() uint64 { return a.length }
func (a alpEncodedArray[T, I]) PType() PType   { return array.PTypeForType[I]() }

func (a alpEncodedArray[T, I]) Slice(start, end uint64) (array.Array[I], error) {
	return materializeSlice(a, start, end)
}

func (a alpEncodedArray[T, I]) WriteTo(w io.Writer) (int64, error) {
	bodySize := a.length * uint64(array.PTypeForType[I]().ByteWidth())
	n, err := array.Header{
		Version: versionNumber,
		PType:   array.PTypeForType[I](),
		Length:  a.length,
		NBytes:  bodySize,
	}.WriteTo(w)
	if err != nil {
		return n, err
	}
	if a.length == 0 {
		return n, nil
	}

	const chunkElems = 1024
	buf := make([]I, chunkElems)
	width := int(unsafe.Sizeof(I(0)))
	var written int64
	for offset := uint64(0); offset < a.length; {
		chunk := len(buf)
		if remaining := a.length - offset; remaining < uint64(chunk) {
			chunk = int(remaining)
		}
		for i := range chunk {
			buf[i] = a.encode(a.valueAt(offset+uint64(i)), a.expE, a.expF)
		}
		bytes := unsafe.Slice((*byte)(unsafe.Pointer(&buf[0])), chunk*width)
		wn, err := w.Write(bytes)
		written += int64(wn)
		if err != nil {
			return n + written, err
		}
		if wn != len(bytes) {
			return n + written, io.ErrShortWrite
		}
		offset += uint64(chunk)
	}
	return n + written, nil
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
	length  uint64
	expE    uint8
	expF    uint8
	encoded EncodedArray[I]
	patches *patches[T, J]
	decode  func(I, uint8, uint8) T
}

const alpBodySize = 2

func (a *alpArray[T, I, J]) Encoding() CodeType { return CodecTypeALP }
func (a *alpArray[T, I, J]) Length() uint64      { return a.length }
func (a *alpArray[T, I, J]) PType() PType        { return array.PTypeForType[T]() }
func (a *alpArray[T, I, J]) BinarySize() uint64 {
	size := uint64(headerSize) + alpBodySize + a.encoded.BinarySize()
	if a.patches != nil {
		size += a.patches.BinarySize()
	}
	return size
}

func (a *alpArray[T, I, J]) ValueAt(offset uint64) T {
	if offset >= a.length {
		panic(errOffsetOutOfRange)
	}
	if value, ok := a.patches.ValueAt(offset); ok {
		return value
	}
	return a.decode(a.encoded.ValueAt(offset), a.expE, a.expF)
}

func (a *alpArray[T, I, J]) DecompressInto(dst []T) error {
	if err := checkDstLen(dst, a.length); err != nil {
		return err
	}
	encoded, err := Decompress(a.encoded)
	if err != nil {
		return err
	}
	for i, value := range encoded {
		dst[i] = a.decode(value, a.expE, a.expF)
	}
	return a.patches.Apply(dst[:a.length])
}


func (a *alpArray[T, I, J]) Slice(start, end uint64) (EncodedArray[T], error) {
	if err := array.ValidateSliceBounds(a.length, start, end); err != nil {
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
	return &alpArray[T, I, J]{length: end - start, expE: a.expE, expF: a.expF, encoded: encoded, patches: patches, decode: a.decode}, nil
}

func (a *alpArray[T, I, J]) WriteTo(w io.Writer) (int64, error) {
	flags := uint32(0)
	if a.patches != nil {
		flags |= flagALPHasPatches
	}
	n, err := header{
		Version:  versionNumber,
		Kind:     CodecTypeALP,
		ElemType: array.PTypeForType[T](),
		Flags:    flags,
		Length:   a.length,
		BodySize: alpBodySize,
	}.WriteTo(w)
	if err != nil {
		return n, err
	}

	var buf [2]byte
	buf[0] = a.expE
	buf[1] = a.expF
	nn, err := w.Write(buf[:2])
	n += int64(nn)
	if err != nil {
		return n, err
	}
	if nn != len(buf) {
		return n, io.ErrShortWrite
	}

	nn64, err := a.encoded.WriteTo(w)
	n += nn64
	if err != nil {
		return n, err
	}
	if a.patches != nil {
		nn64, err = a.patches.WriteTo(w)
		n += nn64
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

func readAnyALPArray[T Integer | Float | String](r io.Reader, h header, opts ReadOptions) (EncodedArray[T], error) {
	var zero T
	switch any(zero).(type) {
	case float64:
		c, err := readALPArrayTyped[float64, int64](r, h, opts, alpDecode64)
		if err != nil {
			return nil, err
		}
		return any(c).(EncodedArray[T]), nil
	case float32:
		c, err := readALPArrayTyped[float32, int32](r, h, opts, alpDecode32)
		if err != nil {
			return nil, err
		}
		return any(c).(EncodedArray[T]), nil
	default:
		return nil, fmt.Errorf("codec: ALP not supported for %v", h.ElemType)
	}
}

func readALPArrayTyped[T Float, I SignedInteger](r io.Reader, h header, opts ReadOptions, decode func(I, uint8, uint8) T) (EncodedArray[T], error) {
	if h.Flags&^flagALPHasPatches != 0 {
		return nil, fmt.Errorf("codec: unsupported ALP flags = 0x%x", h.Flags)
	}
	if h.BodySize != alpBodySize {
		return nil, fmt.Errorf("codec: ALP body size = %d, want %d", h.BodySize, alpBodySize)
	}
	var buf [2]byte
	if _, err := io.ReadFull(r, buf[:2]); err != nil {
		return nil, err
	}
	encoded, err := readEncodedArray[I](r, opts)
	if err != nil {
		return nil, err
	}
	if encoded.Length() != h.Length {
		return nil, fmt.Errorf("codec: ALP length = %d, want %d", h.Length, encoded.Length())
	}

	if h.Flags&flagALPHasPatches == 0 {
		return &alpArray[T, I, uint64]{
			length:  h.Length,
			expE:    buf[0],
			expF:    buf[1],
			encoded: encoded,
			decode:  decode,
		}, nil
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
		return readALPWithPatchIdx[T, I, uint8](r, h, opts, buf, encoded, decode, offset, idxHeader)
	case PTypeUint16:
		return readALPWithPatchIdx[T, I, uint16](r, h, opts, buf, encoded, decode, offset, idxHeader)
	case PTypeUint32:
		return readALPWithPatchIdx[T, I, uint32](r, h, opts, buf, encoded, decode, offset, idxHeader)
	case PTypeUint64:
		return readALPWithPatchIdx[T, I, uint64](r, h, opts, buf, encoded, decode, offset, idxHeader)
	default:
		return nil, fmt.Errorf("codec: ALP patch index type = %v, want unsigned integer", idxHeader.ElemType)
	}
}

func readALPWithPatchIdx[T Float, I SignedInteger, J UnsignedInteger](r io.Reader, h header, opts ReadOptions, buf [2]byte, encoded EncodedArray[I], decode func(I, uint8, uint8) T, offset uint64, idxHeader header) (EncodedArray[T], error) {
	idxCodec, err := readEncodedArrayWithHeader[J](r, idxHeader, opts)
	if err != nil {
		return nil, err
	}
	valCodec, err := readEncodedArray[T](r, opts)
	if err != nil {
		return nil, err
	}
	patches, err := newPatches[T, J](h.Length, offset, idxCodec, valCodec)
	if err != nil {
		return nil, prefixPatchError(err, "ALP")
	}
	return &alpArray[T, I, J]{
		length:  h.Length,
		expE:    buf[0],
		expF:    buf[1],
		encoded: encoded,
		patches: patches,
		decode:  decode,
	}, nil
}

func alpChildContext(ctx planContext) planContext {
	childCtx := ctx.descend()
	if ctx.excludesFloat(CodecTypeDict) {
		childCtx = childCtx.withIntegerExcludes(CodecTypeDict)
	}
	if ctx.excludesFloat(CodecTypeRunEnd) {
		childCtx = childCtx.withIntegerExcludes(CodecTypeRunEnd)
	}
	return childCtx
}

func buildALPArray[T Float](arr array.ArrayCore[T], ctx planContext) (EncodedArray[T], error) {
	if ctx.depth <= 0 {
		return nil, errDepthExhausted
	}
	if arr.Length() == 0 {
		return nil, errDataEmpty
	}

	var zero T
	switch any(zero).(type) {
	case float64:
		c, err := buildALPArrayTyped(any(arr).(array.Array[float64]), ctx, alpFuncs64)
		if err != nil {
			return nil, err
		}
		return any(c).(EncodedArray[T]), nil
	case float32:
		c, err := buildALPArrayTyped(any(arr).(array.Array[float32]), ctx, alpFuncs32)
		if err != nil {
			return nil, err
		}
		return any(c).(EncodedArray[T]), nil
	default:
		return nil, fmt.Errorf("codec: ALP not supported for %T", zero)
	}
}

func buildALPArrayTyped[T Float, I SignedInteger](arr array.ArrayCore[T], ctx planContext, funcs alpFuncs[T, I]) (EncodedArray[T], error) {
	childCtx := alpChildContext(ctx)
	e, f := funcs.findBest(arr)
	n := arr.Length()

	patchIdx := make([]uint64, 0)
	patchVals := make([]T, 0)
	for i := uint64(0); i < n; i++ {
		value := arr.ValueAt(i)
		if funcs.isException(value, e, f) {
			patchIdx = append(patchIdx, i)
			patchVals = append(patchVals, value)
		}
	}
	if uint64(len(patchIdx))*2 > n {
		return nil, errALPHighPatchRatio
	}

	child, err := compressArray[I](alpEncodedArray[T, I]{
		length:  n,
		expE:    e,
		expF:    f,
		valueAt: arr.ValueAt,
		encode:  funcs.encode,
	}, childCtx)
	if err != nil {
		return nil, err
	}

	if len(patchIdx) == 0 {
		return &alpArray[T, I, uint64]{length: n, expE: e, expF: f, encoded: child, decode: funcs.decode}, nil
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
		return buildALPWithPatches[T, I, uint8](n, e, f, child, funcs, patchIdx, patchVals, ctx)
	case maxIdx <= uint64(^uint16(0)):
		return buildALPWithPatches[T, I, uint16](n, e, f, child, funcs, patchIdx, patchVals, ctx)
	case maxIdx <= uint64(^uint32(0)):
		return buildALPWithPatches[T, I, uint32](n, e, f, child, funcs, patchIdx, patchVals, ctx)
	default:
		return buildALPWithPatches[T, I, uint64](n, e, f, child, funcs, patchIdx, patchVals, ctx)
	}
}

func buildALPWithPatches[T Float, I SignedInteger, J UnsignedInteger](n uint64, e, f uint8, child EncodedArray[I], funcs alpFuncs[T, I], patchIdx []uint64, patchVals []T, ctx planContext) (EncodedArray[T], error) {
	patches, err := buildALPPatchesTyped[T, J](n, patchIdx, patchVals, ctx, funcs.toBits)
	if err != nil {
		return nil, err
	}
	return &alpArray[T, I, J]{length: n, expE: e, expF: f, encoded: child, patches: patches, decode: funcs.decode}, nil
}

func buildALPPatchesTyped[T Float, J UnsignedInteger](length uint64, patchIdx []uint64, patchVals []T, ctx planContext, toBits func(T) uint64) (*patches[T, J], error) {
	narrow := make([]J, len(patchIdx))
	for i, v := range patchIdx {
		narrow[i] = J(v)
	}
	patchIdxCodec, err := compressArray(array.NewPrimitivesUnsafe(narrow), ctx.descend().withIntegerExcludes(CodecTypeDict, CodecTypeRunEnd))
	if err != nil {
		return nil, err
	}
	if isAllSameFloat(patchVals, toBits) {
		patchValCodec, err := newConstFloatArray(array.NewPrimitivesUnsafe(patchVals))
		if err != nil {
			return nil, err
		}
		return newPatches[T, J](length, 0, patchIdxCodec, patchValCodec)
	}
	return newPatches[T, J](length, 0, patchIdxCodec, newRawArray(array.NewPrimitivesUnsafe(patchVals)))
}

func estimateALP[T Float, S statsSource[T]](isConst bool) func(S, planContext) (float64, bool) {
	return func(stats S, ctx planContext) (float64, bool) {
		if ctx.depth <= 0 || isConst {
			return 0, false
		}
		return estimateBySample(stats, ctx, buildALPArray[T])
	}
}
