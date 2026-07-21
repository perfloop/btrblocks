package btrblocks

import (
	"errors"
	"fmt"
	"io"
	"math"
	"unsafe"

	"github.com/axiomhq/btrblocks/array"
)

var (
	ErrBuilderRequired       = errors.New("codec: recursive builder is required")
	ErrDepthExhausted        = errors.New("depth exhausted")
	ErrDataEmpty             = errors.New("data is empty")
	ErrValueNotConstant      = errors.New("not constant")
	ErrNotArithmeticSequence = errors.New("not an arithmetic sequence")
	ErrALPHighPatchRatio     = errors.New("codec: ALP patch ratio exceeds 50%")
	ErrALPRDHighPatchRatio   = errors.New("codec: ALPRD patch ratio exceeds 50%")
	ErrMaterializationLimit  = errors.New("codec: materialization limit exceeded")
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
	CodecTypeNullable // MUST always be last: values are the wire encoding; append new codecs here.
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

// excludeSet is a bitmask of CodecType values used by the selector.
type excludeSet uint32

func (s excludeSet) Has(kind CodecType) bool { return s&(1<<kind) != 0 }

func (s excludeSet) With(kinds ...CodecType) excludeSet {
	for _, kind := range kinds {
		s |= 1 << kind
	}
	return s
}

// childBuilder compresses one typed structural child for a parent scheme.
// The caller owns child selection policy; codec builders only produce the
// transformed child input and assemble the resulting encoding node.
type childBuilder[T Integer | Float | String] func(array.ArrayCore[T]) (EncodedArray[T], error)

// unsignedChildBuilder handles a structural child whose physical width is
// selected from its maximum value.
type unsignedChildBuilder interface {
	BuildUint8(array.ArrayCore[uint8]) (EncodedArray[uint8], error)
	BuildUint16(array.ArrayCore[uint16]) (EncodedArray[uint16], error)
	BuildUint32(array.ArrayCore[uint32]) (EncodedArray[uint32], error)
	BuildUint64(array.ArrayCore[uint64]) (EncodedArray[uint64], error)
}

// EncodedArray is a typed node in the primitive/string compression tree.
type EncodedArray[T Integer | Float | String] interface {
	io.WriterTo
	CodecType() CodecType
	ValueAt(offset uint64) T
	Slice(start, end uint64) (EncodedArray[T], error)
	BinarySize() uint64
	Length() uint64
	IsValid(offset uint64) bool
	NullCount() uint64
	PType() PType
	DecompressInto(dst []T) error
	DecodedBytes() (uint64, error)
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

// Decompress materializes e, rejecting outputs larger than 64 MiB before
// allocating. Call DecompressTrusted only for locally produced data whose
// decoded size is already governed by a higher-level memory budget.
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
