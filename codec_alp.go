package btrblocks

import (
	"fmt"
	"io"
	"math"
	"sort"
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

type alpCodec64 struct {
	length    uint64
	expE      uint8
	expF      uint8
	encoded   Codec[int64]
	patchIdxC Codec[uint32]
	patchValC Codec[float64]
	patchIdx  []uint32
	patchVals []float64
}

type alpCodec32 struct {
	length    uint64
	expE      uint8
	expF      uint8
	encoded   Codec[int32]
	patchIdxC Codec[uint32]
	patchValC Codec[float32]
	patchIdx  []uint32
	patchVals []float32
}

const alpBodySize = 2

func (a *alpCodec64) Kind() CodeType { return CodecTypeALP }
func (a *alpCodec64) Length() uint64 { return a.length }
func (a *alpCodec64) PType() PType   { return pTypeForType[float64]() }
func (a *alpCodec64) BinarySize() uint64 {
	size := uint64(headerSize) + alpBodySize + a.encoded.BinarySize()
	if a.patchIdxC != nil {
		size += a.patchIdxC.BinarySize() + a.patchValC.BinarySize()
	}
	return size
}

func (a *alpCodec64) ValueAt(offset uint64) float64 {
	if offset >= a.length {
		panic(errOffsetOutOfRange)
	}
	idx := sort.Search(len(a.patchIdx), func(i int) bool {
		return a.patchIdx[i] >= uint32(offset)
	})
	if idx < len(a.patchIdx) && a.patchIdx[idx] == uint32(offset) {
		return a.patchVals[idx]
	}
	return alpDecode64(a.encoded.ValueAt(offset), a.expE, a.expF)
}

func (a *alpCodec64) Decode(dst []float64) error {
	if err := validateDecodeLength(a.length, len(dst)); err != nil {
		return err
	}
	encoded := make([]int64, a.length)
	if err := a.encoded.Decode(encoded); err != nil {
		return err
	}
	for i, value := range encoded {
		dst[i] = alpDecode64(value, a.expE, a.expF)
	}
	for i, idx := range a.patchIdx {
		dst[int(idx)] = a.patchVals[i]
	}
	return nil
}

func (a *alpCodec64) WriteTo(w io.Writer) (int64, error) {
	flags := uint32(0)
	if a.patchIdxC != nil {
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
	if a.patchIdxC != nil {
		nn64, err = a.patchIdxC.WriteTo(w)
		n += nn64
		if err != nil {
			return n, err
		}
		nn64, err = a.patchValC.WriteTo(w)
		n += nn64
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

func (a *alpCodec32) Kind() CodeType { return CodecTypeALP }
func (a *alpCodec32) Length() uint64 { return a.length }
func (a *alpCodec32) PType() PType   { return pTypeForType[float32]() }
func (a *alpCodec32) BinarySize() uint64 {
	size := uint64(headerSize) + alpBodySize + a.encoded.BinarySize()
	if a.patchIdxC != nil {
		size += a.patchIdxC.BinarySize() + a.patchValC.BinarySize()
	}
	return size
}

func (a *alpCodec32) ValueAt(offset uint64) float32 {
	if offset >= a.length {
		panic(errOffsetOutOfRange)
	}
	idx := sort.Search(len(a.patchIdx), func(i int) bool {
		return a.patchIdx[i] >= uint32(offset)
	})
	if idx < len(a.patchIdx) && a.patchIdx[idx] == uint32(offset) {
		return a.patchVals[idx]
	}
	return alpDecode32(a.encoded.ValueAt(offset), a.expE, a.expF)
}

func (a *alpCodec32) Decode(dst []float32) error {
	if err := validateDecodeLength(a.length, len(dst)); err != nil {
		return err
	}
	encoded := make([]int32, a.length)
	if err := a.encoded.Decode(encoded); err != nil {
		return err
	}
	for i, value := range encoded {
		dst[i] = alpDecode32(value, a.expE, a.expF)
	}
	for i, idx := range a.patchIdx {
		dst[int(idx)] = a.patchVals[i]
	}
	return nil
}

func (a *alpCodec32) WriteTo(w io.Writer) (int64, error) {
	flags := uint32(0)
	if a.patchIdxC != nil {
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
	if a.patchIdxC != nil {
		nn64, err = a.patchIdxC.WriteTo(w)
		n += nn64
		if err != nil {
			return n, err
		}
		nn64, err = a.patchValC.WriteTo(w)
		n += nn64
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

func readAnyALPCodec[T Integer | Float | String](r io.Reader, h header) (Codec[T], error) {
	var zero T
	switch any(zero).(type) {
	case float64:
		c, err := readALPCodec64(r, h)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	case float32:
		c, err := readALPCodec32(r, h)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	default:
		return nil, fmt.Errorf("codec: ALP not supported for %v", h.ElemType)
	}
}

func readALPCodec64(r io.Reader, h header) (Codec[float64], error) {
	if h.BodySize != alpBodySize {
		return nil, fmt.Errorf("codec: ALP body size = %d, want %d", h.BodySize, alpBodySize)
	}
	var buf [2]byte
	if _, err := io.ReadFull(r, buf[:2]); err != nil {
		return nil, err
	}
	encoded, err := readCodec[int64](r)
	if err != nil {
		return nil, err
	}
	if encoded.Length() != h.Length {
		return nil, fmt.Errorf("codec: ALP length = %d, want %d", h.Length, encoded.Length())
	}

	codec := &alpCodec64{
		length:  h.Length,
		expE:    buf[0],
		expF:    buf[1],
		encoded: encoded,
	}

	if h.Flags&flagALPHasPatches != 0 {
		idxCodec, err := readCodec[uint32](r)
		if err != nil {
			return nil, err
		}
		valCodec, err := readCodec[float64](r)
		if err != nil {
			return nil, err
		}
		if idxCodec.Length() != valCodec.Length() {
			return nil, fmt.Errorf("codec: ALP patch length mismatch %d vs %d", idxCodec.Length(), valCodec.Length())
		}
		codec.patchIdxC = idxCodec
		codec.patchValC = valCodec
		codec.patchIdx = make([]uint32, idxCodec.Length())
		if err := idxCodec.Decode(codec.patchIdx); err != nil {
			return nil, err
		}
		codec.patchVals = make([]float64, valCodec.Length())
		if err := valCodec.Decode(codec.patchVals); err != nil {
			return nil, err
		}
	}
	return codec, nil
}

func readALPCodec32(r io.Reader, h header) (Codec[float32], error) {
	if h.BodySize != alpBodySize {
		return nil, fmt.Errorf("codec: ALP body size = %d, want %d", h.BodySize, alpBodySize)
	}
	var buf [2]byte
	if _, err := io.ReadFull(r, buf[:2]); err != nil {
		return nil, err
	}
	encoded, err := readCodec[int32](r)
	if err != nil {
		return nil, err
	}
	if encoded.Length() != h.Length {
		return nil, fmt.Errorf("codec: ALP length = %d, want %d", h.Length, encoded.Length())
	}

	codec := &alpCodec32{
		length:  h.Length,
		expE:    buf[0],
		expF:    buf[1],
		encoded: encoded,
	}

	if h.Flags&flagALPHasPatches != 0 {
		idxCodec, err := readCodec[uint32](r)
		if err != nil {
			return nil, err
		}
		valCodec, err := readCodec[float32](r)
		if err != nil {
			return nil, err
		}
		if idxCodec.Length() != valCodec.Length() {
			return nil, fmt.Errorf("codec: ALP patch length mismatch %d vs %d", idxCodec.Length(), valCodec.Length())
		}
		codec.patchIdxC = idxCodec
		codec.patchValC = valCodec
		codec.patchIdx = make([]uint32, idxCodec.Length())
		if err := idxCodec.Decode(codec.patchIdx); err != nil {
			return nil, err
		}
		codec.patchVals = make([]float32, valCodec.Length())
		if err := valCodec.Decode(codec.patchVals); err != nil {
			return nil, err
		}
	}
	return codec, nil
}

func buildALPCodec[T Float](arr array.Array[T], ctx planContext) (Codec[T], error) {
	if ctx.depth <= 0 {
		return nil, errDepthExhausted
	}
	if arr.Length() == 0 {
		return nil, errDataEmpty
	}

	var zero T
	switch any(zero).(type) {
	case float64:
		e, f := findBestExponents64(any(arr).(array.Array[float64]))
		n := arr.Length()
		patchIdx := make([]uint32, 0)
		patchVals := make([]float64, 0)
		for i := uint64(0); i < n; i++ {
			value := any(arr).(array.Array[float64]).ValueAt(i)
			if alpIsException64(value, e, f) {
				patchIdx = append(patchIdx, uint32(i))
				patchVals = append(patchVals, value)
			}
		}
		if uint64(len(patchIdx))*2 > n {
			return nil, errALPHighPatchRatio
		}
		child, err := compressSignedArray(alpEncodedArray64{
			length:  n,
			expE:    e,
			expF:    f,
			valueAt: any(arr).(array.Array[float64]).ValueAt,
		}, ctx.descend())
		if err != nil {
			return nil, err
		}
		codec := &alpCodec64{length: n, expE: e, expF: f, encoded: child}
		if len(patchIdx) > 0 {
			codec.patchIdx = patchIdx
			codec.patchVals = patchVals
			patchIdxCodec, patchValCodec, err := buildALPPatches64(patchIdx, patchVals, ctx)
			if err != nil {
				return nil, err
			}
			codec.patchIdxC = patchIdxCodec
			codec.patchValC = patchValCodec
		}
		return any(codec).(Codec[T]), nil
	case float32:
		e, f := findBestExponents32(any(arr).(array.Array[float32]))
		n := arr.Length()
		patchIdx := make([]uint32, 0)
		patchVals := make([]float32, 0)
		for i := uint64(0); i < n; i++ {
			value := any(arr).(array.Array[float32]).ValueAt(i)
			if alpIsException32(value, e, f) {
				patchIdx = append(patchIdx, uint32(i))
				patchVals = append(patchVals, value)
			}
		}
		if uint64(len(patchIdx))*2 > n {
			return nil, errALPHighPatchRatio
		}
		child, err := compressSignedArray(alpEncodedArray32{
			length:  n,
			expE:    e,
			expF:    f,
			valueAt: any(arr).(array.Array[float32]).ValueAt,
		}, ctx.descend())
		if err != nil {
			return nil, err
		}
		codec := &alpCodec32{length: n, expE: e, expF: f, encoded: child}
		if len(patchIdx) > 0 {
			codec.patchIdx = patchIdx
			codec.patchVals = patchVals
			patchIdxCodec, patchValCodec, err := buildALPPatches32(patchIdx, patchVals, ctx)
			if err != nil {
				return nil, err
			}
			codec.patchIdxC = patchIdxCodec
			codec.patchValC = patchValCodec
		}
		return any(codec).(Codec[T]), nil
	default:
		return nil, fmt.Errorf("codec: ALP not supported for %T", zero)
	}
}

func buildALPPatches64(patchIdx []uint32, patchVals []float64, ctx planContext) (Codec[uint32], Codec[float64], error) {
	patchIdxCodec, err := compressUnsignedArray(array.NewPrimitivesUnsafe(patchIdx), ctx.descend().withExcludes(CodecTypeDict, CodecTypeRunEnd, CodecTypeSparse))
	if err != nil {
		return nil, nil, err
	}
	if isAllSameFloat64(patchVals) {
		patchValCodec, err := newConstFloatCodec(array.NewPrimitivesUnsafe(patchVals))
		if err != nil {
			return nil, nil, err
		}
		return patchIdxCodec, patchValCodec, nil
	}
	return patchIdxCodec, newRawCodec(array.NewPrimitivesUnsafe(patchVals)), nil
}

func buildALPPatches32(patchIdx []uint32, patchVals []float32, ctx planContext) (Codec[uint32], Codec[float32], error) {
	patchIdxCodec, err := compressUnsignedArray(array.NewPrimitivesUnsafe(patchIdx), ctx.descend().withExcludes(CodecTypeDict, CodecTypeRunEnd, CodecTypeSparse))
	if err != nil {
		return nil, nil, err
	}
	if isAllSameFloat32(patchVals) {
		patchValCodec, err := newConstFloatCodec(array.NewPrimitivesUnsafe(patchVals))
		if err != nil {
			return nil, nil, err
		}
		return patchIdxCodec, patchValCodec, nil
	}
	return patchIdxCodec, newRawCodec(array.NewPrimitivesUnsafe(patchVals)), nil
}

func estimateALP[T Float](isConst bool) func(array.Array[T], planContext) (float64, bool) {
	return func(arr array.Array[T], ctx planContext) (float64, bool) {
		if ctx.depth <= 0 || isConst {
			return 0, false
		}
		return estimateBySample(arr, ctx, buildALPCodec[T])
	}
}
