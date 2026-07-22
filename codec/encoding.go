package codec

import (
	"encoding"
	"errors"
	"fmt"
	"io"
	"math"
	"unsafe"

	"github.com/axiomhq/btrblocks/array"
)

// These errors are documented sentinels. Functions may wrap them with context;
// callers should classify them with errors.Is rather than equality.
var (
	// ErrBuilderRequired means a recursive codec builder was omitted.
	ErrBuilderRequired = errors.New("codec: recursive builder is required")
	// ErrDepthExhausted means selection reached its configured recursion limit.
	ErrDepthExhausted = errors.New("depth exhausted")
	// ErrDataEmpty means the requested codec cannot represent empty input.
	ErrDataEmpty = errors.New("data is empty")
	// ErrValueNotConstant means constant encoding was requested for varying data.
	ErrValueNotConstant = errors.New("not constant")
	// ErrNotArithmeticSequence means sequence encoding was requested for data
	// that is not a non-constant arithmetic progression.
	ErrNotArithmeticSequence = errors.New("not an arithmetic sequence")
	// ErrALPHighPatchRatio means ALP rejected input with too many exceptions.
	ErrALPHighPatchRatio = errors.New("codec: ALP patch ratio exceeds 50%")
	// ErrALPRDHighPatchRatio means ALP-RD rejected input with too many exceptions.
	ErrALPRDHighPatchRatio = errors.New("codec: ALPRD patch ratio exceeds 50%")
	// ErrMaterializationLimit means a build or decode allocation would exceed
	// its configured byte budget.
	ErrMaterializationLimit = errors.New("codec: materialization limit exceeded")
	// ErrWorkLimit means a decode's validation work exceeded ReadOptions.MaxWork.
	ErrWorkLimit = errors.New("codec: decode work limit exceeded")
)

const defaultMaxMaterializedBytes = 64 << 20

type (
	Integer         = array.Integer
	SignedInteger   = array.SignedInteger
	UnsignedInteger = array.UnsignedInteger
	Float           = array.Float
	String          = array.String
	PType           = array.PType
	ReadOptions     = array.ReadOptions
	// BuildOptions bounds temporary memory used by low-level builders. It is
	// array.BuildOptions: one byte-budget knob for the whole build path.
	BuildOptions = array.BuildOptions
)

type cmpFn[T Integer | Float | String] func(T, T) bool

type sliceArrayCore[T Integer | Float | String] []T

func (a sliceArrayCore[T]) Length() uint64          { return uint64(len(a)) }
func (a sliceArrayCore[T]) ValueAt(offset uint64) T { return a[offset] }
func (a sliceArrayCore[T]) IsValid(offset uint64) bool {
	return allValidAt(a.Length(), offset)
}
func (a sliceArrayCore[T]) NullCount() uint64 { return 0 }

type repeatedArrayCore[T Integer | Float | String] struct {
	length uint64
	value  T
}

func (a repeatedArrayCore[T]) Length() uint64   { return a.length }
func (a repeatedArrayCore[T]) ValueAt(uint64) T { return a.value }
func (a repeatedArrayCore[T]) IsValid(offset uint64) bool {
	return allValidAt(a.Length(), offset)
}
func (a repeatedArrayCore[T]) NullCount() uint64 { return 0 }

func allValidAt(length, offset uint64) bool {
	if offset >= length {
		panic(errOffsetOutOfRange)
	}
	return true
}

// denseRows is the row count of a codec node that stores its length directly
// and never contains nulls. Embedding it promotes the Length/IsValid/NullCount
// triple those codec types otherwise hand-roll.
type denseRows uint64

func (d denseRows) Length() uint64             { return uint64(d) }
func (d denseRows) IsValid(offset uint64) bool { return allValidAt(uint64(d), offset) }
func (d denseRows) NullCount() uint64          { return 0 }

// decodeLimit is the materialization budget a node applies to every decode it
// initiates on its own: the whole-node decode Slice needs when a codec cannot
// slice structurally, and the child decodes DecompressInto performs. A node read
// from a stream carries the limit that stream was loaded with, so tightening
// ReadOptions.MaxDecodedBytes bounds the decodes the resulting arrays perform
// later and not merely the one Load did. The zero value — what an in-process
// builder leaves — is the same default Decompress applies, so a locally built
// node is never more permissive than a loaded one.
type decodeLimit uint64

func (d decodeLimit) maxDecodedBytes() uint64 {
	if d == 0 {
		return defaultMaxMaterializedBytes
	}
	return uint64(d)
}

const (
	PTypeUnknown = array.PTypeUnknown
	PTypeInt8    = array.PTypeInt8
	PTypeInt16   = array.PTypeInt16
	PTypeInt32   = array.PTypeInt32
	PTypeInt64   = array.PTypeInt64
	PTypeUint8   = array.PTypeUint8
	PTypeUint16  = array.PTypeUint16
	PTypeUint32  = array.PTypeUint32
	PTypeUint64  = array.PTypeUint64
	PTypeFloat32 = array.PTypeFloat32
	PTypeFloat64 = array.PTypeFloat64
	PTypeString  = array.PTypeString
)

// CodecType identifies the wire-format codec used by an encoded array node.
type CodecType uint8

const (
	CodecTypeUnknown CodecType = iota
	CodecTypeConst
	CodecTypeRaw
	CodecTypeDict
	CodecTypeRunEnd
	CodecTypeZigZag
	CodecTypeBitpack
	CodecTypeFor
	CodecTypeSparse
	CodecTypeSequence
	CodecTypeALP
	CodecTypeFSST
	CodecTypeALPRD
	CodecTypeDelta
	CodecTypeNullable
	// Before v1.0.0, intentional changes must update FORMAT.md and the framing
	// tests. The first release makes existing values append-only.
)

func (k CodecType) String() string {
	switch k {
	case CodecTypeConst:
		return "const"
	case CodecTypeRaw:
		return "raw"
	case CodecTypeDict:
		return "dict"
	case CodecTypeRunEnd:
		return "runend"
	case CodecTypeZigZag:
		return "zigzag"
	case CodecTypeBitpack:
		return "bitpack"
	case CodecTypeFor:
		return "for"
	case CodecTypeSparse:
		return "sparse"
	case CodecTypeSequence:
		return "sequence"
	case CodecTypeALP:
		return "alp"
	case CodecTypeFSST:
		return "fsst"
	case CodecTypeALPRD:
		return "alprd"
	case CodecTypeDelta:
		return "delta"
	case CodecTypeNullable:
		return "nullable"
	default:
		return "unknown"
	}
}

// encodedNode seals EncodedArray. Every codec node embeds it, and only this
// package can, so EncodedArray may gain a method without silently breaking an
// out-of-tree implementation. It is zero-sized; embed it first so it never
// forces trailing padding.
type encodedNode struct{}

func (encodedNode) sealedEncodedArray() {}

type binaryWritable interface {
	io.WriterTo
	BinarySize() uint64
}

// fixedWriter serializes into one allocation sized from BinarySize. It never
// grows: a writer that emits more than it declared fails with io.ErrShortWrite
// instead of turning an incorrect size into a second, unbounded allocation.
type fixedWriter struct {
	buf []byte
	off int
}

func (w *fixedWriter) Write(p []byte) (int, error) {
	n := copy(w.buf[w.off:], p)
	w.off += n
	if n != len(p) {
		return n, io.ErrShortWrite
	}
	return n, nil
}

func marshalBinary(encoded binaryWritable) ([]byte, error) {
	size := encoded.BinarySize()
	if size > uint64(^uint(0)>>1) {
		return nil, fmt.Errorf("codec: marshal binary size %d overflows int", size)
	}

	data := make([]byte, int(size))
	w := fixedWriter{buf: data}
	n, err := encoded.WriteTo(&w)
	if err != nil {
		return nil, fmt.Errorf("codec: marshal binary: %w", err)
	}
	if n != int64(size) {
		return nil, fmt.Errorf("codec: marshal binary wrote %d bytes, want %d", n, size)
	}
	if w.off != len(data) {
		return nil, fmt.Errorf("codec: marshal binary buffered %d bytes, want %d", w.off, size)
	}
	return data, nil
}

// EncodedArray is a typed node in the primitive/string compression tree.
// Implementations live in this package: the interface is sealed, so it is safe
// to type-switch on it and safe for it to grow.
type EncodedArray[T Integer | Float | String] interface {
	// MarshalBinary returns a newly allocated complete wire-format encoding.
	// It serializes the codec tree without decompressing it and does not retain
	// the returned bytes. For large outputs, prefer WriteTo to avoid allocating
	// BinarySize bytes contiguously.
	encoding.BinaryMarshaler
	io.WriterTo
	sealedEncodedArray()
	CodecType() CodecType
	ValueAt(offset uint64) T
	Slice(start, end uint64) (EncodedArray[T], error)
	BinarySize() uint64
	Length() uint64
	IsValid(offset uint64) bool
	NullCount() uint64
	PType() PType
	DecompressInto(dst []T) error
	// DecodedBytes reports the memory one full DecompressInto holds live: the
	// destination buffer plus every intermediate buffer this node and its
	// descendants allocate while filling it. It is a whole-subtree number by
	// contract, because Decompress checks it once before the root allocation:
	// a node that counted only its own destination would let each nested
	// decode claim the limit again, and depth would multiply peak memory.
	DecodedBytes() (uint64, error)
}

// decodeFootprint sums the buffers one DecompressInto holds live at once. Each
// DecodedBytes implementation states its own destination and adds the children
// it materializes; the overflow check lives here rather than in fourteen
// implementations.
type decodeFootprint struct {
	bytes uint64
	err   error
}

func (f *decodeFootprint) add(bytes uint64, err error) {
	switch {
	case f.err != nil: // keep the first failure
	case err != nil:
		f.err = err
	case bytes > ^uint64(0)-f.bytes:
		f.err = errors.New("codec: decoded size overflows")
	default:
		f.bytes += bytes
	}
}

func (f *decodeFootprint) result() (uint64, error) {
	if f.err != nil {
		return 0, f.err
	}
	return f.bytes, nil
}

type dictionaryView[T Integer | Float | String] interface {
	NumDictionaryValues() uint64
	DecompressDictionaryInto(dst []T) error
	VisitMatchingOrdinals(matches []bool, visit func(offset uint64)) error
}

// asDictionary returns a view when encoded's root codec is a dictionary.
func asDictionary[T Integer | Float | String](encoded EncodedArray[T]) (dictionaryView[T], bool) {
	view, ok := encoded.(dictionaryView[T])
	return view, ok
}

// Decompress materializes e, rejecting it before allocating anything when the
// decode's footprint exceeds 64 MiB. DecodedBytes covers the whole subtree, so
// this single check also bounds every nested Decompress the decode performs.
// Call DecompressTrusted only for locally produced data whose decoded size is
// already governed by a higher-level memory budget.
func Decompress[T Integer | Float | String](e EncodedArray[T]) ([]T, error) {
	return decompress(e, defaultMaxMaterializedBytes)
}

// DecompressTrusted materializes e without the conservative decoded-byte
// limit. The encoded length is still checked against the platform's int size.
func DecompressTrusted[T Integer | Float | String](e EncodedArray[T]) ([]T, error) {
	return decompress(e, math.MaxUint64)
}

func decompress[T Integer | Float | String](e EncodedArray[T], maxBytes uint64) ([]T, error) {
	if e == nil {
		return nil, errors.New("codec: nil encoded array")
	}
	length := e.Length()
	if length > uint64(^uint(0)>>1) {
		return nil, fmt.Errorf("codec: materialized length %d overflows int", length)
	}
	decodedBytes, err := e.DecodedBytes()
	if err != nil {
		return nil, err
	}
	if decodedBytes > maxBytes {
		return nil, fmt.Errorf("%w: decoded payload %d exceeds %d bytes", ErrMaterializationLimit, decodedBytes, maxBytes)
	}
	dst := make([]T, length)
	return dst, e.DecompressInto(dst)
}

func decodedBytesFor(length uint64, pType PType) (uint64, error) {
	width := uint64(pType.ByteWidth())
	if pType == PTypeString {
		width = uint64(unsafe.Sizeof(""))
	}
	if width != 0 && length > ^uint64(0)/width {
		return 0, fmt.Errorf("codec: decoded size overflows for %d %s values", length, pType)
	}
	return length * width, nil
}
