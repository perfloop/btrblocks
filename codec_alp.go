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

func findBestExponents64(arr array.Array[float64]) (uint8, uint8) {
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

func findBestExponents32(arr array.Array[float32]) (uint8, uint8) {
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

// alpEncodedArray64 exposes ALP-encoded float64 values as an int64 leaf array.
type alpEncodedArray64 struct {
	length  uint64
	expE    uint8
	expF    uint8
	valueAt func(uint64) float64
}

func (a alpEncodedArray64) ValueAt(offset uint64) int64 {
	return alpEncode64(a.valueAt(offset), a.expE, a.expF)
}

func (a alpEncodedArray64) CopyTo(dst []int64) {
	for i := range dst {
		dst[i] = alpEncode64(a.valueAt(uint64(i)), a.expE, a.expF)
	}
}

func (a alpEncodedArray64) BinarySize() uint64 {
	return primitiveArrayHeaderSize + a.length*uint64(pTypeForType[int64]().ByteWidth())
}

func (a alpEncodedArray64) Length() uint64 { return a.length }
func (a alpEncodedArray64) PType() PType   { return pTypeForType[int64]() }

func (a alpEncodedArray64) Slice(start, end uint64) (array.Array[int64], error) {
	return materializeSlice[int64](a, start, end)
}

func (a alpEncodedArray64) WriteTo(w io.Writer) (int64, error) {
	bodySize := a.length * uint64(pTypeForType[int64]().ByteWidth())
	n, err := array.Header{
		Version:  versionNumber,
		PType:    array.PTypeForType[int64](),
		Length:   a.length,
		BodySize: bodySize,
	}.WriteTo(w)
	if err != nil {
		return n, err
	}
	if a.length == 0 {
		return n, nil
	}

	const chunkElems = 1024
	buf := make([]int64, chunkElems)
	width := int(unsafe.Sizeof(int64(0)))
	var written int64
	for offset := uint64(0); offset < a.length; {
		chunk := len(buf)
		if remaining := a.length - offset; remaining < uint64(chunk) {
			chunk = int(remaining)
		}
		for i := 0; i < chunk; i++ {
			buf[i] = alpEncode64(a.valueAt(offset+uint64(i)), a.expE, a.expF)
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

// alpEncodedArray32 exposes ALP-encoded float32 values as an int32 leaf array.
type alpEncodedArray32 struct {
	length  uint64
	expE    uint8
	expF    uint8
	valueAt func(uint64) float32
}

func (a alpEncodedArray32) ValueAt(offset uint64) int32 {
	return alpEncode32(a.valueAt(offset), a.expE, a.expF)
}

func (a alpEncodedArray32) CopyTo(dst []int32) {
	for i := range dst {
		dst[i] = alpEncode32(a.valueAt(uint64(i)), a.expE, a.expF)
	}
}

func (a alpEncodedArray32) BinarySize() uint64 {
	return primitiveArrayHeaderSize + a.length*uint64(pTypeForType[int32]().ByteWidth())
}

func (a alpEncodedArray32) Length() uint64 { return a.length }
func (a alpEncodedArray32) PType() PType   { return pTypeForType[int32]() }

func (a alpEncodedArray32) Slice(start, end uint64) (array.Array[int32], error) {
	return materializeSlice[int32](a, start, end)
}

func (a alpEncodedArray32) WriteTo(w io.Writer) (int64, error) {
	bodySize := a.length * uint64(pTypeForType[int32]().ByteWidth())
	n, err := array.Header{
		Version:  versionNumber,
		PType:    array.PTypeForType[int32](),
		Length:   a.length,
		BodySize: bodySize,
	}.WriteTo(w)
	if err != nil {
		return n, err
	}
	if a.length == 0 {
		return n, nil
	}

	const chunkElems = 1024
	buf := make([]int32, chunkElems)
	width := int(unsafe.Sizeof(int32(0)))
	var written int64
	for offset := uint64(0); offset < a.length; {
		chunk := len(buf)
		if remaining := a.length - offset; remaining < uint64(chunk) {
			chunk = int(remaining)
		}
		for i := 0; i < chunk; i++ {
			buf[i] = alpEncode32(a.valueAt(offset+uint64(i)), a.expE, a.expF)
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

func isAllSameFloat64(values []float64) bool {
	if len(values) == 0 {
		return true
	}
	bits0 := math.Float64bits(values[0])
	for _, value := range values[1:] {
		if math.Float64bits(value) != bits0 {
			return false
		}
	}
	return true
}

func isAllSameFloat32(values []float32) bool {
	if len(values) == 0 {
		return true
	}
	bits0 := math.Float32bits(values[0])
	for _, value := range values[1:] {
		if math.Float32bits(value) != bits0 {
			return false
		}
	}
	return true
}

// alpArray64 stores an ALP-encoded int64 child plus optional float64 exceptions.
type alpArray64 struct {
	length  uint64
	expE    uint8
	expF    uint8
	encoded EncodedArray[int64]
	patches *patches[float64]
}

// alpArray32 stores an ALP-encoded int32 child plus optional float32 exceptions.
type alpArray32 struct {
	length  uint64
	expE    uint8
	expF    uint8
	encoded EncodedArray[int32]
	patches *patches[float32]
}

const alpBodySize = 2

func (a *alpArray64) Encoding() CodeType { return CodecTypeALP }
func (a *alpArray64) Length() uint64     { return a.length }
func (a *alpArray64) PType() PType       { return pTypeForType[float64]() }
func (a *alpArray64) BinarySize() uint64 {
	size := uint64(headerSize) + alpBodySize + a.encoded.BinarySize()
	if a.patches != nil {
		size += a.patches.BinarySize()
	}
	return size
}

func (a *alpArray64) ValueAt(offset uint64) float64 {
	if offset >= a.length {
		panic(errOffsetOutOfRange)
	}
	if value, ok := a.patches.ValueAt(offset); ok {
		return value
	}
	return alpDecode64(a.encoded.ValueAt(offset), a.expE, a.expF)
}

func (a *alpArray64) CopyTo(dst []float64) error {
	if err := validateCopyLength(a.length, len(dst)); err != nil {
		return err
	}
	encoded := make([]int64, a.length)
	if err := a.encoded.CopyTo(encoded); err != nil {
		return err
	}
	for i, value := range encoded {
		dst[i] = alpDecode64(value, a.expE, a.expF)
	}
	return a.patches.Apply(dst)
}

func (a *alpArray64) Slice(start, end uint64) (EncodedArray[float64], error) {
	if err := validateSliceBounds(a.length, start, end); err != nil {
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
	return &alpArray64{length: end - start, expE: a.expE, expF: a.expF, encoded: encoded, patches: patches}, nil
}

func (a *alpArray64) WriteTo(w io.Writer) (int64, error) {
	flags := uint32(0)
	if a.patches != nil {
		flags |= flagALPHasPatches
	}
	n, err := header{
		Version:  versionNumber,
		Kind:     CodecTypeALP,
		ElemType: pTypeForType[float64](),
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

func (a *alpArray32) Encoding() CodeType { return CodecTypeALP }
func (a *alpArray32) Length() uint64     { return a.length }
func (a *alpArray32) PType() PType       { return pTypeForType[float32]() }
func (a *alpArray32) BinarySize() uint64 {
	size := uint64(headerSize) + alpBodySize + a.encoded.BinarySize()
	if a.patches != nil {
		size += a.patches.BinarySize()
	}
	return size
}

func (a *alpArray32) ValueAt(offset uint64) float32 {
	if offset >= a.length {
		panic(errOffsetOutOfRange)
	}
	if value, ok := a.patches.ValueAt(offset); ok {
		return value
	}
	return alpDecode32(a.encoded.ValueAt(offset), a.expE, a.expF)
}

func (a *alpArray32) CopyTo(dst []float32) error {
	if err := validateCopyLength(a.length, len(dst)); err != nil {
		return err
	}
	encoded := make([]int32, a.length)
	if err := a.encoded.CopyTo(encoded); err != nil {
		return err
	}
	for i, value := range encoded {
		dst[i] = alpDecode32(value, a.expE, a.expF)
	}
	return a.patches.Apply(dst)
}

func (a *alpArray32) Slice(start, end uint64) (EncodedArray[float32], error) {
	if err := validateSliceBounds(a.length, start, end); err != nil {
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
	return &alpArray32{length: end - start, expE: a.expE, expF: a.expF, encoded: encoded, patches: patches}, nil
}

func (a *alpArray32) WriteTo(w io.Writer) (int64, error) {
	flags := uint32(0)
	if a.patches != nil {
		flags |= flagALPHasPatches
	}
	n, err := header{
		Version:  versionNumber,
		Kind:     CodecTypeALP,
		ElemType: pTypeForType[float32](),
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

func readAnyALPArray[T Integer | Float | String](r io.Reader, h header) (EncodedArray[T], error) {
	var zero T
	switch any(zero).(type) {
	case float64:
		c, err := readALPArray64(r, h)
		if err != nil {
			return nil, err
		}
		return any(c).(EncodedArray[T]), nil
	case float32:
		c, err := readALPArray32(r, h)
		if err != nil {
			return nil, err
		}
		return any(c).(EncodedArray[T]), nil
	default:
		return nil, fmt.Errorf("codec: ALP not supported for %v", h.ElemType)
	}
}

func readALPArray64(r io.Reader, h header) (EncodedArray[float64], error) {
	if h.BodySize != alpBodySize {
		return nil, fmt.Errorf("codec: ALP body size = %d, want %d", h.BodySize, alpBodySize)
	}
	var buf [2]byte
	if _, err := io.ReadFull(r, buf[:2]); err != nil {
		return nil, err
	}
	encoded, err := readEncodedArray[int64](r)
	if err != nil {
		return nil, err
	}
	if encoded.Length() != h.Length {
		return nil, fmt.Errorf("codec: ALP length = %d, want %d", h.Length, encoded.Length())
	}

	codec := &alpArray64{
		length:  h.Length,
		expE:    buf[0],
		expF:    buf[1],
		encoded: encoded,
	}

	if h.Flags&flagALPHasPatches != 0 {
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
		valCodec, err := readEncodedArray[float64](r)
		if err != nil {
			return nil, err
		}
		patches, err := newPatches(h.Length, offset, idxCodec, valCodec)
		if err != nil {
			return nil, prefixPatchError(err, "ALP")
		}
		codec.patches = patches
	}
	return codec, nil
}

func readALPArray32(r io.Reader, h header) (EncodedArray[float32], error) {
	if h.BodySize != alpBodySize {
		return nil, fmt.Errorf("codec: ALP body size = %d, want %d", h.BodySize, alpBodySize)
	}
	var buf [2]byte
	if _, err := io.ReadFull(r, buf[:2]); err != nil {
		return nil, err
	}
	encoded, err := readEncodedArray[int32](r)
	if err != nil {
		return nil, err
	}
	if encoded.Length() != h.Length {
		return nil, fmt.Errorf("codec: ALP length = %d, want %d", h.Length, encoded.Length())
	}

	codec := &alpArray32{
		length:  h.Length,
		expE:    buf[0],
		expF:    buf[1],
		encoded: encoded,
	}

	if h.Flags&flagALPHasPatches != 0 {
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
		valCodec, err := readEncodedArray[float32](r)
		if err != nil {
			return nil, err
		}
		patches, err := newPatches(h.Length, offset, idxCodec, valCodec)
		if err != nil {
			return nil, prefixPatchError(err, "ALP")
		}
		codec.patches = patches
	}
	return codec, nil
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

func buildALPArray[T Float](arr array.Array[T], ctx planContext) (EncodedArray[T], error) {
	if ctx.depth <= 0 {
		return nil, errDepthExhausted
	}
	if arr.Length() == 0 {
		return nil, errDataEmpty
	}

	var zero T
	switch any(zero).(type) {
	case float64:
		childCtx := alpChildContext(ctx)
		e, f := findBestExponents64(any(arr).(array.Array[float64]))
		n := arr.Length()
		patchIdx := make([]uint64, 0)
		patchVals := make([]float64, 0)
		for i := uint64(0); i < n; i++ {
			value := any(arr).(array.Array[float64]).ValueAt(i)
			if alpIsException64(value, e, f) {
				patchIdx = append(patchIdx, i)
				patchVals = append(patchVals, value)
			}
		}
		if uint64(len(patchIdx))*2 > n {
			return nil, errALPHighPatchRatio
		}
		child, err := compressArray[int64](alpEncodedArray64{
			length:  n,
			expE:    e,
			expF:    f,
			valueAt: any(arr).(array.Array[float64]).ValueAt,
		}, childCtx)
		if err != nil {
			return nil, err
		}
		codec := &alpArray64{length: n, expE: e, expF: f, encoded: child}
		if len(patchIdx) > 0 {
			patches, err := buildALPPatches64(n, patchIdx, patchVals, ctx)
			if err != nil {
				return nil, err
			}
			codec.patches = patches
		}
		return any(codec).(EncodedArray[T]), nil
	case float32:
		childCtx := alpChildContext(ctx)
		e, f := findBestExponents32(any(arr).(array.Array[float32]))
		n := arr.Length()
		patchIdx := make([]uint64, 0)
		patchVals := make([]float32, 0)
		for i := uint64(0); i < n; i++ {
			value := any(arr).(array.Array[float32]).ValueAt(i)
			if alpIsException32(value, e, f) {
				patchIdx = append(patchIdx, i)
				patchVals = append(patchVals, value)
			}
		}
		if uint64(len(patchIdx))*2 > n {
			return nil, errALPHighPatchRatio
		}
		child, err := compressArray[int32](alpEncodedArray32{
			length:  n,
			expE:    e,
			expF:    f,
			valueAt: any(arr).(array.Array[float32]).ValueAt,
		}, childCtx)
		if err != nil {
			return nil, err
		}
		codec := &alpArray32{length: n, expE: e, expF: f, encoded: child}
		if len(patchIdx) > 0 {
			patches, err := buildALPPatches32(n, patchIdx, patchVals, ctx)
			if err != nil {
				return nil, err
			}
			codec.patches = patches
		}
		return any(codec).(EncodedArray[T]), nil
	default:
		return nil, fmt.Errorf("codec: ALP not supported for %T", zero)
	}
}

func buildALPPatches64(length uint64, patchIdx []uint64, patchVals []float64, ctx planContext) (*patches[float64], error) {
	patchIdxCodec, err := buildCompressedOrdinals(patchIdx, ctx.descend(), CodecTypeDict, CodecTypeRunEnd)
	if err != nil {
		return nil, err
	}
	if isAllSameFloat64(patchVals) {
		patchValCodec, err := newConstFloatArray(array.NewPrimitivesUnsafe(patchVals))
		if err != nil {
			return nil, err
		}
		return newPatches(length, 0, patchIdxCodec, patchValCodec)
	}
	return newPatches(length, 0, patchIdxCodec, newRawArray(array.NewPrimitivesUnsafe(patchVals)))
}

func buildALPPatches32(length uint64, patchIdx []uint64, patchVals []float32, ctx planContext) (*patches[float32], error) {
	patchIdxCodec, err := buildCompressedOrdinals(patchIdx, ctx.descend(), CodecTypeDict, CodecTypeRunEnd)
	if err != nil {
		return nil, err
	}
	if isAllSameFloat32(patchVals) {
		patchValCodec, err := newConstFloatArray(array.NewPrimitivesUnsafe(patchVals))
		if err != nil {
			return nil, err
		}
		return newPatches(length, 0, patchIdxCodec, patchValCodec)
	}
	return newPatches(length, 0, patchIdxCodec, newRawArray(array.NewPrimitivesUnsafe(patchVals)))
}

func estimateALP[T Float, S statsSource[T]](isConst bool) func(S, planContext) (float64, bool) {
	return func(stats S, ctx planContext) (float64, bool) {
		if ctx.depth <= 0 || isConst {
			return 0, false
		}
		return estimateBySample(stats, ctx, buildALPArray[T])
	}
}
