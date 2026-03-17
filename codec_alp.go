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

func alpEncode64(value float64, e, f uint8) int64 {
	return int64(math.Round(value * pow10F64[e] * ipow10F64[f]))
}

func alpDecode64(encoded int64, e, f uint8) float64 {
	return float64(encoded) * ipow10F64[e] * pow10F64[f]
}

func alpIsException64(value float64, e, f uint8) bool {
	encoded := alpEncode64(value, e, f)
	decoded := alpDecode64(encoded, e, f)
	return math.Float64bits(decoded) != math.Float64bits(value)
}

func alpEncode32(value float32, e, f uint8) int32 {
	return int32(math.Round(float64(value) * float64(pow10F32[e]) * float64(ipow10F32[f])))
}

func alpDecode32(encoded int32, e, f uint8) float32 {
	return float32(encoded) * ipow10F32[e] * pow10F32[f]
}

func alpIsException32(value float32, e, f uint8) bool {
	encoded := alpEncode32(value, e, f)
	decoded := alpDecode32(encoded, e, f)
	return math.Float32bits(decoded) != math.Float32bits(value)
}

func findBestExponents64(arr array.Array[float64]) (uint8, uint8) {
	n := arr.Length()
	step := uint64(1)
	if n > 64 {
		step = n / 64
	}

	var (
		bestE, bestF   uint8
		bestExceptions = uint64(math.MaxUint64)
	)
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

	var (
		bestE, bestF   uint8
		bestExceptions = uint64(math.MaxUint64)
	)
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

// alpEncodedArray64 is a lazy array adapter for ALP-encoded float64 values.
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
		Version:  1,
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
	var (
		buf     = make([]int64, chunkElems)
		width   = int(unsafe.Sizeof(int64(0)))
		written int64
	)
	for offset := uint64(0); offset < a.length; {
		chunk := len(buf)
		remaining := a.length - offset
		if remaining < uint64(chunk) {
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

// alpEncodedArray32 is a lazy array adapter for ALP-encoded float32 values.
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
		Version:  1,
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
	var (
		buf     = make([]int32, chunkElems)
		width   = int(unsafe.Sizeof(int32(0)))
		written int64
	)
	for offset := uint64(0); offset < a.length; {
		chunk := len(buf)
		remaining := a.length - offset
		if remaining < uint64(chunk) {
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

var errALPHighPatchRatio = fmt.Errorf("codec: ALP patch ratio exceeds 50%%")

func isAllSameFloat64(vals []float64) bool {
	if len(vals) == 0 {
		return true
	}
	bits0 := math.Float64bits(vals[0])
	for _, v := range vals[1:] {
		if math.Float64bits(v) != bits0 {
			return false
		}
	}
	return true
}

func isAllSameFloat32(vals []float32) bool {
	if len(vals) == 0 {
		return true
	}
	bits0 := math.Float32bits(vals[0])
	for _, v := range vals[1:] {
		if math.Float32bits(v) != bits0 {
			return false
		}
	}
	return true
}

// ALPCodec64 implements ALP encoding for float64 values, encoding them as int64
// integers via exponent-based multiplication. Exceptions that don't round-trip
// losslessly are stored as compressed patch children.
type ALPCodec64 struct {
	length     uint64
	expE, expF uint8
	encoded    Codec[int64]    // child 0: compressed encoded integers
	patchIdxC  Codec[uint32]   // child 1: compressed exception indices (nil if none)
	patchValC  Codec[float64]  // child 2: compressed exception values (nil if none)
	patchIdx   []uint32        // decoded cache for fast ValueAt
	patchVals  []float64       // decoded cache for fast ValueAt
}

// ALPCodec32 implements ALP encoding for float32 values, encoding them as int32
// integers via exponent-based multiplication. Exceptions that don't round-trip
// losslessly are stored as compressed patch children.
type ALPCodec32 struct {
	length     uint64
	expE, expF uint8
	encoded    Codec[int32]    // child 0: compressed encoded integers
	patchIdxC  Codec[uint32]   // child 1: compressed exception indices (nil if none)
	patchValC  Codec[float32]  // child 2: compressed exception values (nil if none)
	patchIdx   []uint32        // decoded cache for fast ValueAt
	patchVals  []float32       // decoded cache for fast ValueAt
}

const alpBodySize = 2 // expE (1 byte) + expF (1 byte)

func NewALPCodec[T Float](arr array.Array[T], depth int, excludes codecExcludes) (Codec[T], error) {
	if depth <= 0 {
		return nil, errDepthExhausted
	}
	if arr.Length() == 0 {
		return nil, errDataEmpty
	}

	var zero T
	switch any(zero).(type) {
	case float64:
		c, err := newALPCodec64(any(arr).(array.Array[float64]), depth, excludes)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	case float32:
		c, err := newALPCodec32(any(arr).(array.Array[float32]), depth, excludes)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	default:
		return nil, fmt.Errorf("codec: ALP not supported for type %T", zero)
	}
}

func newALPCodec64(arr array.Array[float64], depth int, excludes codecExcludes) (*ALPCodec64, error) {
	e, f := findBestExponents64(arr)
	n := arr.Length()

	var (
		patchIdx  []uint32
		patchVals []float64
	)
	for i := uint64(0); i < n; i++ {
		v := arr.ValueAt(i)
		if alpIsException64(v, e, f) {
			patchIdx = append(patchIdx, uint32(i))
			patchVals = append(patchVals, v)
		}
	}

	if uint64(len(patchIdx))*2 > n {
		return nil, errALPHighPatchRatio
	}

	childExcl := excludes.with(CodecTypeALP)
	encoded := CompressInteger(alpEncodedArray64{
		length: n, expE: e, expF: f, valueAt: arr.ValueAt,
	}, depth-1, childExcl)

	var (
		patchIdxC Codec[uint32]
		patchValC Codec[float64]
	)
	if len(patchIdx) > 0 {
		// Patches are kept uncompressed for linear indexing.
		// Only narrow indices and check constant values.
		patchIdxC = NewRawCodec(array.NewPrimitivesUnsafe(patchIdx))
		if isAllSameFloat64(patchVals) {
			patchValC, _ = NewConstFloatCodec(array.NewPrimitivesUnsafe(patchVals))
		} else {
			patchValC = NewRawCodec(array.NewPrimitivesUnsafe(patchVals))
		}
	}

	return &ALPCodec64{
		length:    n,
		expE:      e,
		expF:      f,
		encoded:   encoded,
		patchIdxC: patchIdxC,
		patchValC: patchValC,
		patchIdx:  patchIdx,
		patchVals: patchVals,
	}, nil
}

func newALPCodec32(arr array.Array[float32], depth int, excludes codecExcludes) (*ALPCodec32, error) {
	e, f := findBestExponents32(arr)
	n := arr.Length()

	var (
		patchIdx  []uint32
		patchVals []float32
	)
	for i := uint64(0); i < n; i++ {
		v := arr.ValueAt(i)
		if alpIsException32(v, e, f) {
			patchIdx = append(patchIdx, uint32(i))
			patchVals = append(patchVals, v)
		}
	}

	if uint64(len(patchIdx))*2 > n {
		return nil, errALPHighPatchRatio
	}

	childExcl := excludes.with(CodecTypeALP)
	encoded := CompressInteger(alpEncodedArray32{
		length: n, expE: e, expF: f, valueAt: arr.ValueAt,
	}, depth-1, childExcl)

	var (
		patchIdxC Codec[uint32]
		patchValC Codec[float32]
	)
	if len(patchIdx) > 0 {
		// Patches are kept uncompressed for linear indexing.
		// Only narrow indices and check constant values.
		patchIdxC = NewRawCodec(array.NewPrimitivesUnsafe(patchIdx))
		if isAllSameFloat32(patchVals) {
			patchValC, _ = NewConstFloatCodec(array.NewPrimitivesUnsafe(patchVals))
		} else {
			patchValC = NewRawCodec(array.NewPrimitivesUnsafe(patchVals))
		}
	}

	return &ALPCodec32{
		length:    n,
		expE:      e,
		expF:      f,
		encoded:   encoded,
		patchIdxC: patchIdxC,
		patchValC: patchValC,
		patchIdx:  patchIdx,
		patchVals: patchVals,
	}, nil
}

// --- ALPCodec64 Codec interface ---

func (a *ALPCodec64) ValueAt(offset uint64) (float64, error) {
	if offset >= a.length {
		return 0, errOffsetOutOfRange
	}
	idx := sort.Search(len(a.patchIdx), func(i int) bool {
		return a.patchIdx[i] >= uint32(offset)
	})
	if idx < len(a.patchIdx) && a.patchIdx[idx] == uint32(offset) {
		return a.patchVals[idx], nil
	}
	enc, err := a.encoded.ValueAt(offset)
	if err != nil {
		return 0, err
	}
	return alpDecode64(enc, a.expE, a.expF), nil
}

func (a *ALPCodec64) Decode(dst []float64) error {
	if err := validateDecodeLength(a.length, len(dst)); err != nil {
		return err
	}
	encoded := make([]int64, a.length)
	if err := a.encoded.Decode(encoded); err != nil {
		return err
	}
	for i, enc := range encoded {
		dst[i] = alpDecode64(enc, a.expE, a.expF)
	}
	for i, idx := range a.patchIdx {
		dst[idx] = a.patchVals[i]
	}
	return nil
}

func (a *ALPCodec64) Children() []Scheme {
	if a.patchIdxC == nil {
		return []Scheme{a.encoded}
	}
	return []Scheme{a.encoded, a.patchIdxC, a.patchValC}
}

func (a *ALPCodec64) Length() uint64 { return a.length }
func (a *ALPCodec64) PType() PType  { return pTypeForType[float64]() }

func (a *ALPCodec64) BinarySize() uint64 {
	size := uint64(headerSize) + alpBodySize + a.encoded.BinarySize()
	if a.patchIdxC != nil {
		size += a.patchIdxC.BinarySize() + a.patchValC.BinarySize()
	}
	return size
}

func (a *ALPCodec64) childCount() uint8 {
	if a.patchIdxC != nil {
		return 3
	}
	return 1
}

func (a *ALPCodec64) WriteTo(w io.Writer) (n int64, err error) {
	n, err = Header{
		Version:    1,
		Kind:       CodecTypeALP,
		ElemType:   pTypeForType[float64](),
		ChildCount: a.childCount(),
		Flags:      0,
		Length:     a.length,
		BodySize:   alpBodySize,
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

	wn, err := a.encoded.WriteTo(w)
	n += wn
	if err != nil {
		return n, err
	}

	if a.patchIdxC != nil {
		wn, err = a.patchIdxC.WriteTo(w)
		n += wn
		if err != nil {
			return n, err
		}
		wn, err = a.patchValC.WriteTo(w)
		n += wn
		if err != nil {
			return n, err
		}
	}

	return n, nil
}

// --- ALPCodec32 Codec interface ---

func (a *ALPCodec32) ValueAt(offset uint64) (float32, error) {
	if offset >= a.length {
		return 0, errOffsetOutOfRange
	}
	idx := sort.Search(len(a.patchIdx), func(i int) bool {
		return a.patchIdx[i] >= uint32(offset)
	})
	if idx < len(a.patchIdx) && a.patchIdx[idx] == uint32(offset) {
		return a.patchVals[idx], nil
	}
	enc, err := a.encoded.ValueAt(offset)
	if err != nil {
		return 0, err
	}
	return alpDecode32(enc, a.expE, a.expF), nil
}

func (a *ALPCodec32) Decode(dst []float32) error {
	if err := validateDecodeLength(a.length, len(dst)); err != nil {
		return err
	}
	encoded := make([]int32, a.length)
	if err := a.encoded.Decode(encoded); err != nil {
		return err
	}
	for i, enc := range encoded {
		dst[i] = alpDecode32(enc, a.expE, a.expF)
	}
	for i, idx := range a.patchIdx {
		dst[idx] = a.patchVals[i]
	}
	return nil
}

func (a *ALPCodec32) Children() []Scheme {
	if a.patchIdxC == nil {
		return []Scheme{a.encoded}
	}
	return []Scheme{a.encoded, a.patchIdxC, a.patchValC}
}

func (a *ALPCodec32) Length() uint64 { return a.length }
func (a *ALPCodec32) PType() PType  { return pTypeForType[float32]() }

func (a *ALPCodec32) BinarySize() uint64 {
	size := uint64(headerSize) + alpBodySize + a.encoded.BinarySize()
	if a.patchIdxC != nil {
		size += a.patchIdxC.BinarySize() + a.patchValC.BinarySize()
	}
	return size
}

func (a *ALPCodec32) childCount() uint8 {
	if a.patchIdxC != nil {
		return 3
	}
	return 1
}

func (a *ALPCodec32) WriteTo(w io.Writer) (n int64, err error) {
	n, err = Header{
		Version:    1,
		Kind:       CodecTypeALP,
		ElemType:   pTypeForType[float32](),
		ChildCount: a.childCount(),
		Flags:      0,
		Length:     a.length,
		BodySize:   alpBodySize,
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

	wn, err := a.encoded.WriteTo(w)
	n += wn
	if err != nil {
		return n, err
	}

	if a.patchIdxC != nil {
		wn, err = a.patchIdxC.WriteTo(w)
		n += wn
		if err != nil {
			return n, err
		}
		wn, err = a.patchValC.WriteTo(w)
		n += wn
		if err != nil {
			return n, err
		}
	}

	return n, nil
}

// --- Read / deserialization ---

func readAnyALPCodec[T Integer | Float | String](r io.Reader, header Header) (Codec[T], error) {
	var zero T
	switch any(zero).(type) {
	case float64:
		c, err := readALPCodec64(r, header)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	case float32:
		c, err := readALPCodec32(r, header)
		if err != nil {
			return nil, err
		}
		return any(c).(Codec[T]), nil
	default:
		return nil, fmt.Errorf("codec: ALP not supported for type %v", header.ElemType)
	}
}

func readALPCodec64(r io.Reader, header Header) (*ALPCodec64, error) {
	if header.ChildCount != 1 && header.ChildCount != 3 {
		return nil, fmt.Errorf("codec: ALP child count = %d, want 1 or 3", header.ChildCount)
	}
	if header.BodySize != alpBodySize {
		return nil, fmt.Errorf("codec: ALP body size = %d, want %d", header.BodySize, alpBodySize)
	}

	var buf [2]byte
	if _, err := io.ReadFull(r, buf[:2]); err != nil {
		return nil, err
	}
	expE := buf[0]
	expF := buf[1]

	encoded, err := readCodec[int64](r)
	if err != nil {
		return nil, err
	}
	if header.Length != encoded.Length() {
		return nil, fmt.Errorf("codec: ALP length = %d, want %d", header.Length, encoded.Length())
	}

	codec := &ALPCodec64{
		length:  header.Length,
		expE:    expE,
		expF:    expF,
		encoded: encoded,
	}

	if header.ChildCount == 3 {
		idxCodec, err := readCodec[uint32](r)
		if err != nil {
			return nil, err
		}
		valCodec, err := readCodec[float64](r)
		if err != nil {
			return nil, err
		}
		if idxCodec.Length() != valCodec.Length() {
			return nil, fmt.Errorf("codec: ALP patch index length = %d, value length = %d", idxCodec.Length(), valCodec.Length())
		}
		codec.patchIdxC = idxCodec
		codec.patchValC = valCodec

		// Decode patches into slices for fast ValueAt.
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

func readALPCodec32(r io.Reader, header Header) (*ALPCodec32, error) {
	if header.ChildCount != 1 && header.ChildCount != 3 {
		return nil, fmt.Errorf("codec: ALP child count = %d, want 1 or 3", header.ChildCount)
	}
	if header.BodySize != alpBodySize {
		return nil, fmt.Errorf("codec: ALP body size = %d, want %d", header.BodySize, alpBodySize)
	}

	var buf [2]byte
	if _, err := io.ReadFull(r, buf[:2]); err != nil {
		return nil, err
	}
	expE := buf[0]
	expF := buf[1]

	encoded, err := readCodec[int32](r)
	if err != nil {
		return nil, err
	}
	if header.Length != encoded.Length() {
		return nil, fmt.Errorf("codec: ALP length = %d, want %d", header.Length, encoded.Length())
	}

	codec := &ALPCodec32{
		length:  header.Length,
		expE:    expE,
		expF:    expF,
		encoded: encoded,
	}

	if header.ChildCount == 3 {
		idxCodec, err := readCodec[uint32](r)
		if err != nil {
			return nil, err
		}
		valCodec, err := readCodec[float32](r)
		if err != nil {
			return nil, err
		}
		if idxCodec.Length() != valCodec.Length() {
			return nil, fmt.Errorf("codec: ALP patch index length = %d, value length = %d", idxCodec.Length(), valCodec.Length())
		}
		codec.patchIdxC = idxCodec
		codec.patchValC = valCodec

		// Decode patches into slices for fast ValueAt.
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
