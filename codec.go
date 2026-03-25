package btrblocks

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"unsafe"

	"github.com/axiomhq/btrblocks/array"
)

var (
	errOffsetOutOfRange = errors.New("offset out of range")
	errDepthExhausted   = errors.New("depth exhausted")
	errDataEmpty        = errors.New("data is empty")
)

func checkDstLen[T Integer | Float | String](dst []T, need uint64) error {
	if uint64(len(dst)) < need {
		return fmt.Errorf("codec: dst length = %d, need >= %d", len(dst), need)
	}
	return nil
}

const (
	versionNumber = 1
	headerSize    = 24

	// defaultMaxReadLength and defaultMaxReadBytes are safety limits applied
	// when ReadOptions does not specify explicit bounds. They prevent OOM from
	// adversarial headers while being generous enough for any real workload.
	// Callers can override via ReadOptions.MaxLength / ReadOptions.MaxBytes.
	defaultMaxReadLength = 1 << 36 // ~68 billion elements
	defaultMaxReadBytes  = 1 << 40 // ~1 TB
)

type kindSet uint16

func (e kindSet) has(kind CodeType) bool {
	return e&(1<<kind) != 0
}

func (e kindSet) with(kinds ...CodeType) kindSet {
	for _, kind := range kinds {
		e |= 1 << kind
	}
	return e
}

// plannerExcludes groups per-domain encoding exclusions for recursive planning.
type plannerExcludes struct {
	integers kindSet
	floats   kindSet
	strings  kindSet
}

// planContext carries planner depth, sampling state, and recursive exclusions.
type planContext struct {
	depth    int
	isSample bool
	excludes plannerExcludes
}

func newPlanContext(opts Options) planContext {
	opts = normalizeOptions(opts)
	return planContext{depth: opts.MaxDepth, excludes: opts.excludes}
}

func (c planContext) descend() planContext {
	if c.depth > 0 {
		c.depth--
	}
	return c
}

func (c planContext) sampled() planContext {
	c.isSample = true
	return c
}

func (c planContext) withIntegerExcludes(kinds ...CodeType) planContext {
	c.excludes.integers = c.excludes.integers.with(kinds...)
	return c
}

func (c planContext) withFloatExcludes(kinds ...CodeType) planContext {
	c.excludes.floats = c.excludes.floats.with(kinds...)
	return c
}

func (c planContext) withStringExcludes(kinds ...CodeType) planContext {
	c.excludes.strings = c.excludes.strings.with(kinds...)
	return c
}

func withTypeExcludes[T Integer | Float | String](ctx planContext, kinds ...CodeType) planContext {
	var zero T
	switch any(zero).(type) {
	case string:
		return ctx.withStringExcludes(kinds...)
	case float32, float64:
		return ctx.withFloatExcludes(kinds...)
	default:
		return ctx.withIntegerExcludes(kinds...)
	}
}

func (c planContext) excludesInteger(kind CodeType) bool {
	return c.excludes.integers.has(kind)
}

func (c planContext) excludesFloat(kind CodeType) bool {
	return c.excludes.floats.has(kind)
}

func (c planContext) excludesString(kind CodeType) bool {
	return c.excludes.strings.has(kind)
}

// EncodedArray is a typed encoded leaf node in the primitive/string compression tree.
//
// String aliasing: For string-typed arrays, values returned by ValueAt and
// written into dst by DecompressInto may alias internal codec memory (e.g.
// FSST decode buffers or raw array backing storage). Callers that retain
// string values beyond the EncodedArray's lifetime must copy them.
type EncodedArray[T Integer | Float | String] interface {
	io.WriterTo
	Encoding() CodeType
	ValueAt(offset uint64) T
	Slice(start, end uint64) (EncodedArray[T], error)
	BinarySize() uint64
	Length() uint64
	PType() PType
	// DecompressInto recursively decodes into dst, which must have length >= Length().
	DecompressInto(dst []T) error
}

// Decompress recursively decodes an encoded array into a newly allocated slice.
func Decompress[T Integer | Float | String](e EncodedArray[T]) ([]T, error) {
	dst := make([]T, e.Length())
	return dst, e.DecompressInto(dst)
}

// writeVirtualArray writes a header + chunked transformed values to w.
// Used by forEncodedArray, zigzagEncodedArray, and alpEncodedArray which all
// need to write a virtual array (source values passed through a transform)
// without materializing the entire result.
func writeVirtualArray[T Integer | Float](w io.Writer, length uint64, transform func(uint64) T) (int64, error) {
	bodySize := length * uint64(array.PTypeForType[T]().ByteWidth())
	n, err := array.Header{
		Version:  versionNumber,
		PType:    array.PTypeForType[T](),
		Length:   length,
		NumBytes: bodySize,
	}.WriteTo(w)
	if err != nil {
		return n, err
	}
	if length == 0 {
		return n, nil
	}

	chunkElems := uint64(1024)
	if length < chunkElems {
		chunkElems = length
	}
	width := int(unsafe.Sizeof(T(0)))
	buf := make([]byte, int(chunkElems)*width)

	var written int64
	for offset := uint64(0); offset < length; {
		chunk := int(chunkElems)
		if remaining := length - offset; remaining < uint64(chunk) {
			chunk = int(remaining)
		}
		putLittleEndian(buf, width, chunk, func(i int) uint64 {
			return uint64(transform(offset + uint64(i)))
		})
		wn, err := w.Write(buf[:chunk*width])
		written += int64(wn)
		if err != nil {
			return n + written, err
		}
		if wn != chunk*width {
			return n + written, io.ErrShortWrite
		}
		offset += uint64(chunk)
	}
	return n + written, nil
}

// writeIntegerLE writes a single integer value as little-endian bytes to w.
// Used by sequence and FoR codecs to serialize inline scalar fields.
func writeIntegerLE[T Integer](w io.Writer, value T) (int64, error) {
	var buf [8]byte
	switch unsafe.Sizeof(value) {
	case 1:
		buf[0] = byte(value)
		n, err := w.Write(buf[:1])
		return int64(n), err
	case 2:
		binary.LittleEndian.PutUint16(buf[:2], uint16(value))
		n, err := w.Write(buf[:2])
		return int64(n), err
	case 4:
		binary.LittleEndian.PutUint32(buf[:4], uint32(value))
		n, err := w.Write(buf[:4])
		return int64(n), err
	default:
		binary.LittleEndian.PutUint64(buf[:8], uint64(value))
		n, err := w.Write(buf[:8])
		return int64(n), err
	}
}

// readIntegerLE reads a single integer value from little-endian bytes.
func readIntegerLE[T Integer](data []byte) T {
	switch unsafe.Sizeof(T(0)) {
	case 1:
		return T(data[0])
	case 2:
		return T(binary.LittleEndian.Uint16(data[:2]))
	case 4:
		return T(binary.LittleEndian.Uint32(data[:4]))
	default:
		return T(binary.LittleEndian.Uint64(data[:8]))
	}
}

// codecHeader is the fixed 24-byte encoded-array stream prefix written before
// each codec node body. Distinct from array.Header (20 bytes) which prefixes
// raw array bodies within the codec tree.
//
// Version is checked on read and must equal versionNumber (currently 1).
// Changing a codec's serialization format requires bumping versionNumber and
// adding migration logic in readEncodedArrayWithHeader. There is no backward
// compatibility mechanism — a version mismatch is a hard error.
//
// BodySize is the number of bytes of inline data written directly after this
// header and before any recursive EncodedArray children or patch streams.
// For leaf-wrapping codecs (raw, const) this equals the wrapped array.Array's
// BinarySize (which includes the array header). For codecs that delegate to
// recursive children (dict, runend, zigzag) BodySize is 0. For codecs with
// fixed inline fields followed by children (for, bitpack, alp, alprd, fsst,
// sequence) BodySize covers only the inline portion.
type codecHeader struct {
	Version  uint8
	Kind     CodeType
	ElemType PType
	Reserved uint8
	Flags    uint32
	Length   uint64
	NumBytes uint64
}

func readHeader(br *array.BufReader) (codecHeader, error) {
	buf, err := br.Read(headerSize)
	if err != nil {
		return codecHeader{}, err
	}
	return codecHeader{
		Version:  buf[0],
		Kind:     CodeType(buf[1]),
		ElemType: PType(buf[2]),
		Reserved: buf[3],
		Flags:    binary.LittleEndian.Uint32(buf[4:8]),
		Length:   binary.LittleEndian.Uint64(buf[8:16]),
		NumBytes: binary.LittleEndian.Uint64(buf[16:24]),
	}, nil
}

func (h codecHeader) WriteTo(w io.Writer) (int64, error) {
	var buf [headerSize]byte
	buf[0] = h.Version
	buf[1] = byte(h.Kind)
	buf[2] = byte(h.ElemType)
	buf[3] = h.Reserved
	binary.LittleEndian.PutUint32(buf[4:8], h.Flags)
	binary.LittleEndian.PutUint64(buf[8:16], h.Length)
	binary.LittleEndian.PutUint64(buf[16:24], h.NumBytes)
	n, err := w.Write(buf[:])
	if err == nil && n != len(buf) {
		err = io.ErrShortWrite
	}
	return int64(n), err
}

// putLittleEndian encodes count values of the given byte width into buf using
// explicit little-endian byte order. This avoids platform-endianness assumptions
// from unsafe pointer casts.
func putLittleEndian(buf []byte, width, count int, value func(int) uint64) {
	switch width {
	case 1:
		for i := range count {
			buf[i] = byte(value(i))
		}
	case 2:
		for i := range count {
			binary.LittleEndian.PutUint16(buf[i*2:], uint16(value(i)))
		}
	case 4:
		for i := range count {
			binary.LittleEndian.PutUint32(buf[i*4:], uint32(value(i)))
		}
	case 8:
		for i := range count {
			binary.LittleEndian.PutUint64(buf[i*8:], value(i))
		}
	}
}

func materializeSlice[T Integer | Float | String](src interface {
	Length() uint64
	ValueAt(uint64) T
}, start, end uint64) (array.Array[T], error) {
	if err := array.ValidateSliceBounds(src.Length(), start, end); err != nil {
		return nil, err
	}
	values := make([]T, end-start)
	for i := range values {
		values[i] = src.ValueAt(start + uint64(i))
	}
	return buildArray(values), nil
}

func sliceToRawArray[T Integer | Float | String](src interface {
	Length() uint64
	ValueAt(uint64) T
}, start, end uint64) (EncodedArray[T], error) {
	values, err := materializeSlice(src, start, end)
	if err != nil {
		return nil, err
	}
	return newRawArray(values), nil
}

func validateHeaderForType[T Integer | Float | String](h codecHeader, opts ReadOptions) error {
	if h.Version != versionNumber {
		return fmt.Errorf("codec: unsupported version = %d", h.Version)
	}
	if h.Reserved != 0 {
		return fmt.Errorf("codec: reserved byte = %d, want 0", h.Reserved)
	}
	switch h.Kind {
	case CodecTypeConst, CodecTypeRaw, CodecTypeDict, CodecTypeRunEnd, CodecTypeZigZag, CodecTypeBitpack, CodecTypeFor, CodecTypeSequence, CodecTypeALP, CodecTypeFSST, CodecTypeALPRD:
	default:
		return fmt.Errorf("codec: unknown kind = %d", h.Kind)
	}
	// Codecs that use flags (bitpack, ALP, ALPRD) validate their own flags
	// in their read functions. All other codecs must have flags == 0.
	switch h.Kind {
	case CodecTypeBitpack, CodecTypeALP, CodecTypeALPRD:
		// Validated per-codec.
	default:
		if h.Flags != 0 {
			return fmt.Errorf("codec: unsupported flags = 0x%x for %s", h.Flags, h.Kind)
		}
	}
	expected := array.PTypeForType[T]()
	if h.ElemType != expected {
		return fmt.Errorf("codec: element type = %v, want %v", h.ElemType, expected)
	}
	maxLen := opts.MaxLength
	if maxLen == 0 {
		maxLen = defaultMaxReadLength
	}
	if h.Length > maxLen {
		return fmt.Errorf("codec: length %d exceeds limit %d", h.Length, maxLen)
	}
	maxBytes := opts.MaxBytes
	if maxBytes == 0 {
		maxBytes = defaultMaxReadBytes
	}
	if h.NumBytes > maxBytes {
		return fmt.Errorf("codec: body size %d exceeds limit %d", h.NumBytes, maxBytes)
	}
	return nil
}

// readCast converts a typed EncodedArray result to the generic EncodedArray[T].
// Used by readAny* dispatch functions to avoid repeating the error-check + cast
// pattern in every type-switch arm.
func readCast[T Integer | Float | String](result any, err error) (EncodedArray[T], error) {
	if err != nil {
		return nil, err
	}
	return result.(EncodedArray[T]), nil
}

func readEncodedArray[T Integer | Float | String](br *array.BufReader, opts ReadOptions) (EncodedArray[T], error) {
	h, err := readHeader(br)
	if err != nil {
		return nil, err
	}
	return readEncodedArrayWithHeader[T](br, h, opts)
}

func readEncodedArrayWithHeader[T Integer | Float | String](br *array.BufReader, h codecHeader, opts ReadOptions) (EncodedArray[T], error) {
	if err := validateHeaderForType[T](h, opts); err != nil {
		return nil, err
	}
	switch h.Kind {
	case CodecTypeConst:
		return readConstArray[T](br, h, opts)
	case CodecTypeRaw:
		return readRawArray[T](br, h, opts)
	case CodecTypeDict:
		return readDictArray[T](br, h, opts)
	case CodecTypeRunEnd:
		return readRunEndArray[T](br, h, opts)
	case CodecTypeZigZag:
		return readAnyZigZagArray[T](br, h, opts)
	case CodecTypeBitpack:
		return readAnyBitPackedArray[T](br, h, opts)
	case CodecTypeFor:
		return readAnyFoRArray[T](br, h, opts)
	case CodecTypeSequence:
		return readAnySequenceArray[T](br, h, opts)
	case CodecTypeALP:
		return readAnyALPArray[T](br, h, opts)
	case CodecTypeALPRD:
		return readAnyALPRDArray[T](br, h, opts)
	case CodecTypeFSST:
		return readAnyFSSTArray[T](br, h, opts)
	default:
		return nil, fmt.Errorf("codec: unknown kind = %d", h.Kind)
	}
}
