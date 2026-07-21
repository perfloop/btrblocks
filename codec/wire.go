package codec

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
	errDataEmpty        = ErrDataEmpty
)

func checkDstLen[T Integer | Float | String](dst []T, need uint64) error {
	if uint64(len(dst)) < need {
		return fmt.Errorf("codec: dst length = %d, need >= %d", len(dst), need)
	}
	return nil
}

func requireNonNullable[T Integer | Float | String](child array.ArrayCore[T], name string) error {
	if child.NullCount() != 0 {
		return fmt.Errorf("codec: %s child contains nulls", name)
	}
	return nil
}

const (
	// FormatVersion is the current codec-tree wire version emitted by writers.
	// Version 1 remains a pre-release draft until the first v1.0.0 tag.
	FormatVersion = 1
	versionNumber = FormatVersion
	headerSize    = 24

	// These match the array decoder's documented zero-value limits.
	defaultMaxReadLength = 1 << 26 // 67 million values.
	defaultMaxReadBytes  = 1 << 30 // 1 GiB body.

	// defaultMaxReadDepth bounds codec-tree nesting when decoding. The build
	// planner caps cascade depth at a handful of levels, but every dict,
	// patch, or FSST child adds nodes beyond the cascade count, so the decode
	// limit is deliberately generous. Without it a crafted stream of nested
	// headers drives one stack frame per ~24 bytes of payload.
	defaultMaxReadDepth = 32
)

type encodedReader[T Integer | Float | String] func(*array.BufReader, ReadOptions) (EncodedArray[T], error)

func load[T Integer | Float | String](data []byte, opts []ReadOptions, read encodedReader[T]) (EncodedArray[T], error) {
	br := array.BufReader{Buf: data}
	encoded, err := loadFromBuf(&br, opts, read)
	if err != nil {
		return nil, err
	}
	if remaining := br.Remaining(); remaining != 0 {
		return nil, fmt.Errorf("codec: trailing data: %d bytes", remaining)
	}
	return encoded, nil
}

func loadFromBuf[T Integer | Float | String](br *array.BufReader, opts []ReadOptions, read encodedReader[T]) (EncodedArray[T], error) {
	if br == nil {
		return nil, errors.New("codec: nil buffer reader")
	}
	var option ReadOptions
	if len(opts) > 0 {
		option = opts[0]
	}
	return read(br, option)
}

// LoadSigned deserializes exactly one signed-integer array from data. The
// result may borrow data; keep it alive and unchanged while using the result.
func LoadSigned[T SignedInteger](data []byte, opts ...ReadOptions) (EncodedArray[T], error) {
	return load(data, opts, readSignedEncodedArray[T])
}

// LoadUnsigned deserializes exactly one unsigned-integer array from data. The
// result may borrow data; keep it alive and unchanged while using the result.
func LoadUnsigned[T UnsignedInteger](data []byte, opts ...ReadOptions) (EncodedArray[T], error) {
	return load(data, opts, readUnsignedEncodedArray[T])
}

// LoadFloat32 deserializes exactly one float32 array from data. The result may
// borrow data; keep it alive and unchanged while using the result.
func LoadFloat32(data []byte, opts ...ReadOptions) (EncodedArray[float32], error) {
	return load(data, opts, readFloat32EncodedArray)
}

// LoadFloat64 deserializes exactly one float64 array from data. The result may
// borrow data; keep it alive and unchanged while using the result.
func LoadFloat64(data []byte, opts ...ReadOptions) (EncodedArray[float64], error) {
	return load(data, opts, readFloat64EncodedArray)
}

// LoadStrings deserializes exactly one string array from data. The result may
// borrow data; keep it alive and unchanged while using the result.
func LoadStrings(data []byte, opts ...ReadOptions) (EncodedArray[string], error) {
	return load(data, opts, readStringEncodedArray)
}

// LoadSignedFromBuf deserializes one signed-integer prefix and advances br. The
// result may borrow br.Buf; keep it alive and unchanged while using the result.
func LoadSignedFromBuf[T SignedInteger](br *array.BufReader, opts ...ReadOptions) (EncodedArray[T], error) {
	return loadFromBuf(br, opts, readSignedEncodedArray[T])
}

// LoadUnsignedFromBuf deserializes one unsigned-integer prefix and advances br.
// The result may borrow br.Buf; keep it alive and unchanged while using it.
func LoadUnsignedFromBuf[T UnsignedInteger](br *array.BufReader, opts ...ReadOptions) (EncodedArray[T], error) {
	return loadFromBuf(br, opts, readUnsignedEncodedArray[T])
}

// LoadFloat32FromBuf deserializes one float32 prefix and advances br. The result
// may borrow br.Buf; keep it alive and unchanged while using it.
func LoadFloat32FromBuf(br *array.BufReader, opts ...ReadOptions) (EncodedArray[float32], error) {
	return loadFromBuf(br, opts, readFloat32EncodedArray)
}

// LoadFloat64FromBuf deserializes one float64 prefix and advances br. The result
// may borrow br.Buf; keep it alive and unchanged while using it.
func LoadFloat64FromBuf(br *array.BufReader, opts ...ReadOptions) (EncodedArray[float64], error) {
	return loadFromBuf(br, opts, readFloat64EncodedArray)
}

// LoadStringsFromBuf deserializes one string prefix and advances br. The result
// may borrow br.Buf; keep it alive and unchanged while using it.
func LoadStringsFromBuf(br *array.BufReader, opts ...ReadOptions) (EncodedArray[string], error) {
	return loadFromBuf(br, opts, readStringEncodedArray)
}

// writeSum accumulates the byte count of a sequence of writes to w and
// short-circuits on the first error. It collapses the
// `nn, err := X; n += nn; if err != nil { return n, err }` chain that every
// codec WriteTo otherwise repeats per inline field and child.
type writeSum struct {
	w io.Writer
	n int64
}

// write writes b in full, counting the bytes; a short write is an error.
func (s *writeSum) write(b []byte) error {
	nn, err := s.w.Write(b)
	s.n += int64(nn)
	if err != nil {
		return err
	}
	if nn != len(b) {
		return io.ErrShortWrite
	}
	return nil
}

// writeTo delegates to wt, counting the bytes it reports.
func (s *writeSum) writeTo(wt io.WriterTo) error {
	nn, err := wt.WriteTo(s.w)
	s.n += nn
	return err
}

// add folds a writer helper that already returns a (count, error) pair.
func (s *writeSum) add(nn int64, err error) error {
	s.n += nn
	return err
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
// Version is checked on read and must identify the current format. Version 1
// is a pre-release draft until v1.0.0. That release freezes it; subsequent
// writer versions must add read dispatch without removing released version 1
// support. Unknown versions are hard errors. See FORMAT.md.
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
	Type     CodecType
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
		Type:     CodecType(buf[1]),
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
	buf[1] = byte(h.Type)
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

type sliceBuilder[T Integer | Float | String] func(src array.ArrayCore[T], start, end uint64) (EncodedArray[T], error)

func slicePrimitiveToRawArray[T Integer | Float](src array.ArrayCore[T], start, end uint64) (EncodedArray[T], error) {
	values, err := array.MaterializePrimitiveSlice(src, start, end)
	if err != nil {
		return nil, err
	}
	return newRawArray(values), nil
}

func sliceStringToRawArray(src array.ArrayCore[string], start, end uint64) (EncodedArray[string], error) {
	values, err := array.MaterializeStringSlice(src, start, end)
	if err != nil {
		return nil, err
	}
	return newRawArray(values), nil
}

func validateHeaderForType(h codecHeader, opts ReadOptions, expected PType) error {
	if h.Version != versionNumber {
		return fmt.Errorf("codec: unsupported version = %d", h.Version)
	}
	if h.Reserved != 0 {
		return fmt.Errorf("codec: reserved byte = %d, want 0", h.Reserved)
	}
	switch h.Type {
	case CodecTypeConst, CodecTypeRaw, CodecTypeDict, CodecTypeRunEnd, CodecTypeZigZag, CodecTypeBitpack, CodecTypeFor, CodecTypeSparse, CodecTypeSequence, CodecTypeALP, CodecTypeFSST, CodecTypeALPRD, CodecTypeDelta, CodecTypeNullable:
	default:
		return fmt.Errorf("codec: unknown kind = %d", h.Type)
	}
	// Codecs that use flags (bitpack, ALP, ALPRD) validate their own flags
	// in their read functions. All other codecs must have flags == 0.
	switch h.Type {
	case CodecTypeBitpack, CodecTypeALP, CodecTypeALPRD, CodecTypeSparse:
		// Validated per-
	default:
		if h.Flags != 0 {
			return fmt.Errorf("codec: unsupported flags = 0x%x for %s", h.Flags, h.Type)
		}
	}
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

type encodedSpecialReader[T Integer | Float | String] func(*array.BufReader, codecHeader, ReadOptions) (EncodedArray[T], error)

func readPrimitiveBody[T Integer | Float](br *array.BufReader, opts ...ReadOptions) (array.Array[T], error) {
	return array.ReadPrimitiveFromBuf[T](br, opts...)
}

func readSignedEncodedArray[T SignedInteger](br *array.BufReader, opts ReadOptions) (EncodedArray[T], error) {
	h, err := readHeader(br)
	if err != nil {
		return nil, fmt.Errorf("codec: header: %w", err)
	}
	return readSignedEncodedArrayWithHeader[T](br, h, opts)
}

func readUnsignedEncodedArray[T UnsignedInteger](br *array.BufReader, opts ReadOptions) (EncodedArray[T], error) {
	h, err := readHeader(br)
	if err != nil {
		return nil, fmt.Errorf("codec: header: %w", err)
	}
	return readUnsignedEncodedArrayWithHeader[T](br, h, opts)
}

func readFloat32EncodedArray(br *array.BufReader, opts ReadOptions) (EncodedArray[float32], error) {
	h, err := readHeader(br)
	if err != nil {
		return nil, fmt.Errorf("codec: header: %w", err)
	}
	return readFloat32EncodedArrayWithHeader(br, h, opts)
}

func readFloat64EncodedArray(br *array.BufReader, opts ReadOptions) (EncodedArray[float64], error) {
	h, err := readHeader(br)
	if err != nil {
		return nil, fmt.Errorf("codec: header: %w", err)
	}
	return readFloat64EncodedArrayWithHeader(br, h, opts)
}

func readStringEncodedArray(br *array.BufReader, opts ReadOptions) (EncodedArray[string], error) {
	h, err := readHeader(br)
	if err != nil {
		return nil, fmt.Errorf("codec: header: %w", err)
	}
	return readStringEncodedArrayWithHeader(br, h, opts)
}

func readSignedEncodedArrayWithHeader[T SignedInteger](br *array.BufReader, h codecHeader, opts ReadOptions) (EncodedArray[T], error) {
	return readEncodedArrayWithHeader(br, h, opts, array.PTypeOfPrimitive[T](), readSignedEncodedArray[T], readPrimitiveBody[T], slicePrimitiveToRawArray[T], readSignedSpecial[T])
}

func readUnsignedEncodedArrayWithHeader[T UnsignedInteger](br *array.BufReader, h codecHeader, opts ReadOptions) (EncodedArray[T], error) {
	return readEncodedArrayWithHeader(br, h, opts, array.PTypeOfPrimitive[T](), readUnsignedEncodedArray[T], readPrimitiveBody[T], slicePrimitiveToRawArray[T], readUnsignedSpecial[T])
}

func readFloat32EncodedArrayWithHeader(br *array.BufReader, h codecHeader, opts ReadOptions) (EncodedArray[float32], error) {
	return readEncodedArrayWithHeader(br, h, opts, PTypeFloat32, readFloat32EncodedArray, readPrimitiveBody[float32], slicePrimitiveToRawArray[float32], readFloat32Special)
}

func readFloat64EncodedArrayWithHeader(br *array.BufReader, h codecHeader, opts ReadOptions) (EncodedArray[float64], error) {
	return readEncodedArrayWithHeader(br, h, opts, PTypeFloat64, readFloat64EncodedArray, readPrimitiveBody[float64], slicePrimitiveToRawArray[float64], readFloat64Special)
}

func readStringEncodedArrayWithHeader(br *array.BufReader, h codecHeader, opts ReadOptions) (EncodedArray[string], error) {
	return readEncodedArrayWithHeader(br, h, opts, PTypeString, readStringEncodedArray, array.ReadStringsFromBuf, sliceStringToRawArray, readStringSpecial)
}

func readEncodedArrayWithHeader[T Integer | Float | String](br *array.BufReader, h codecHeader, opts ReadOptions, expected PType, readValues encodedReader[T], readBody arrayBodyReader[T], slice sliceBuilder[T], readSpecial encodedSpecialReader[T]) (EncodedArray[T], error) {
	// Bound input-driven recursion: every codec node consumes one level, and
	// the decremented opts value propagates to all child reads. Zero means
	// "unset" and normalizes to the default; -1 marks exhaustion because a
	// plain decrement to zero would re-normalize in the child.
	if opts.MaxDepth == 0 {
		opts.MaxDepth = defaultMaxReadDepth
	}
	if opts.MaxDepth < 0 {
		return nil, fmt.Errorf("codec: nesting depth exceeds limit")
	}
	if opts.MaxDepth--; opts.MaxDepth == 0 {
		opts.MaxDepth = -1
	}
	if err := validateHeaderForType(h, opts, expected); err != nil {
		return nil, err
	}
	switch h.Type {
	case CodecTypeNullable:
		return readNullableArray(br, h, opts, readValues)
	case CodecTypeConst:
		return readConstArray(br, h, opts, readBody)
	case CodecTypeRaw:
		return readRawArray(br, h, opts, readBody)
	case CodecTypeDict:
		return readDictArray(br, h, opts, readValues)
	case CodecTypeSparse:
		return readSparseArray(br, h, opts, readValues, slice)
	case CodecTypeRunEnd:
		return readRunEndArray(br, h, opts, readValues, slice)
	default:
		return readSpecial(br, h, opts)
	}
}

func readSignedSpecial[T SignedInteger](br *array.BufReader, h codecHeader, opts ReadOptions) (EncodedArray[T], error) {
	if h.Type == CodecTypeZigZag {
		return readZigZagArray[T](br, h, opts)
	}
	return readIntegerArray(br, h, opts, readSignedEncodedArray[T])
}

func readUnsignedSpecial[T UnsignedInteger](br *array.BufReader, h codecHeader, opts ReadOptions) (EncodedArray[T], error) {
	return readIntegerArray(br, h, opts, readUnsignedEncodedArray[T])
}

func readFloat32Special(br *array.BufReader, h codecHeader, opts ReadOptions) (EncodedArray[float32], error) {
	switch h.Type {
	case CodecTypeALP:
		return readALPArrayTyped(br, h, opts, alpFuncs32, readFloat32EncodedArray)
	case CodecTypeALPRD:
		return readALPRDArrayTyped(br, h, opts, alprdFuncs32)
	default:
		return nil, fmt.Errorf("codec: %s is not a float32 codec", h.Type)
	}
}

func readFloat64Special(br *array.BufReader, h codecHeader, opts ReadOptions) (EncodedArray[float64], error) {
	switch h.Type {
	case CodecTypeALP:
		return readALPArrayTyped(br, h, opts, alpFuncs64, readFloat64EncodedArray)
	case CodecTypeALPRD:
		return readALPRDArrayTyped(br, h, opts, alprdFuncs64)
	default:
		return nil, fmt.Errorf("codec: %s is not a float64 codec", h.Type)
	}
}

func readStringSpecial(br *array.BufReader, h codecHeader, opts ReadOptions) (EncodedArray[string], error) {
	if h.Type != CodecTypeFSST {
		return nil, fmt.Errorf("codec: %s is not a string codec", h.Type)
	}
	return readFSSTArray(br, h, opts)
}

func readIntegerArray[U Integer](br *array.BufReader, h codecHeader, opts ReadOptions, readValues encodedReader[U]) (EncodedArray[U], error) {
	switch h.Type {
	case CodecTypeBitpack:
		return readBitPackedArray(br, h, opts, readValues)
	case CodecTypeFor:
		return readFoRArray(br, h, opts, readValues)
	case CodecTypeSequence:
		return readSequenceArray[U](br, h, opts)
	case CodecTypeDelta:
		return readDeltaArray(br, h, opts, readValues)
	default:
		return nil, fmt.Errorf("codec: %s is not an integer codec", h.Type)
	}
}
