package btrblocks

import (
	"github.com/axiomhq/btrblocks/array"
	"github.com/axiomhq/btrblocks/codec"
)

type (
	Integer         = codec.Integer
	SignedInteger   = codec.SignedInteger
	UnsignedInteger = codec.UnsignedInteger
	Float           = codec.Float
	String          = codec.String
	PType           = codec.PType
	ReadOptions     = codec.ReadOptions
)

// CodecType identifies the wire-format codec used by an encoded array node.
type CodecType = codec.CodecType

// EncodedArray is a typed node in the primitive/string compression tree.
type EncodedArray[T Integer | Float | String] = codec.EncodedArray[T]

const (
	// FormatVersion is the current codec-tree wire version emitted by writers.
	FormatVersion = codec.FormatVersion

	// DefaultMaxBuildBytes is the default limit for one temporary build
	// materialization.
	DefaultMaxBuildBytes = codec.DefaultMaxBuildBytes

	PTypeUnknown = codec.PTypeUnknown
	PTypeInt8    = codec.PTypeInt8
	PTypeInt16   = codec.PTypeInt16
	PTypeInt32   = codec.PTypeInt32
	PTypeInt64   = codec.PTypeInt64
	PTypeUint8   = codec.PTypeUint8
	PTypeUint16  = codec.PTypeUint16
	PTypeUint32  = codec.PTypeUint32
	PTypeUint64  = codec.PTypeUint64
	PTypeFloat32 = codec.PTypeFloat32
	PTypeFloat64 = codec.PTypeFloat64
	PTypeString  = codec.PTypeString

	CodecTypeUnknown  = codec.CodecTypeUnknown
	CodecTypeConst    = codec.CodecTypeConst
	CodecTypeRaw      = codec.CodecTypeRaw
	CodecTypeDict     = codec.CodecTypeDict
	CodecTypeRunEnd   = codec.CodecTypeRunEnd
	CodecTypeZigZag   = codec.CodecTypeZigZag
	CodecTypeBitpack  = codec.CodecTypeBitpack
	CodecTypeFor      = codec.CodecTypeFor
	CodecTypeSparse   = codec.CodecTypeSparse
	CodecTypeSequence = codec.CodecTypeSequence
	CodecTypeALP      = codec.CodecTypeALP
	CodecTypeFSST     = codec.CodecTypeFSST
	CodecTypeALPRD    = codec.CodecTypeALPRD
	CodecTypeDelta    = codec.CodecTypeDelta
	CodecTypeNullable = codec.CodecTypeNullable
)

// These aliases preserve codec's sentinel identities. Returned errors
// may wrap them; callers should classify failures with errors.Is.
var (
	ErrBuilderRequired       = codec.ErrBuilderRequired
	ErrDepthExhausted        = codec.ErrDepthExhausted
	ErrDataEmpty             = codec.ErrDataEmpty
	ErrValueNotConstant      = codec.ErrValueNotConstant
	ErrNotArithmeticSequence = codec.ErrNotArithmeticSequence
	ErrALPHighPatchRatio     = codec.ErrALPHighPatchRatio
	ErrALPRDHighPatchRatio   = codec.ErrALPRDHighPatchRatio
	ErrMaterializationLimit  = codec.ErrMaterializationLimit
	ErrWorkLimit             = codec.ErrWorkLimit
)

// LoadSigned deserializes exactly one signed-integer array from data. The
// result may borrow data; keep it alive and unchanged while using the result.
func LoadSigned[T SignedInteger](data []byte, opts ...ReadOptions) (EncodedArray[T], error) {
	return codec.LoadSigned[T](data, opts...)
}

// LoadUnsigned deserializes exactly one unsigned-integer array from data. The
// result may borrow data; keep it alive and unchanged while using the result.
func LoadUnsigned[T UnsignedInteger](data []byte, opts ...ReadOptions) (EncodedArray[T], error) {
	return codec.LoadUnsigned[T](data, opts...)
}

// LoadFloat32 deserializes exactly one float32 array from data. The result may
// borrow data; keep it alive and unchanged while using the result.
func LoadFloat32(data []byte, opts ...ReadOptions) (EncodedArray[float32], error) {
	return codec.LoadFloat32(data, opts...)
}

// LoadFloat64 deserializes exactly one float64 array from data. The result may
// borrow data; keep it alive and unchanged while using the result.
func LoadFloat64(data []byte, opts ...ReadOptions) (EncodedArray[float64], error) {
	return codec.LoadFloat64(data, opts...)
}

// LoadStrings deserializes exactly one string array from data. The result may
// borrow data; keep it alive and unchanged while using the result.
func LoadStrings(data []byte, opts ...ReadOptions) (EncodedArray[string], error) {
	return codec.LoadStrings(data, opts...)
}

// LoadSignedFromBuf deserializes one signed-integer prefix and advances br. The
// result may borrow br.Buf; keep it alive and unchanged while using the result.
func LoadSignedFromBuf[T SignedInteger](br *array.BufReader, opts ...ReadOptions) (EncodedArray[T], error) {
	return codec.LoadSignedFromBuf[T](br, opts...)
}

// LoadUnsignedFromBuf deserializes one unsigned-integer prefix and advances br.
// The result may borrow br.Buf; keep it alive and unchanged while using it.
func LoadUnsignedFromBuf[T UnsignedInteger](br *array.BufReader, opts ...ReadOptions) (EncodedArray[T], error) {
	return codec.LoadUnsignedFromBuf[T](br, opts...)
}

// LoadFloat32FromBuf deserializes one float32 prefix and advances br. The result
// may borrow br.Buf; keep it alive and unchanged while using it.
func LoadFloat32FromBuf(br *array.BufReader, opts ...ReadOptions) (EncodedArray[float32], error) {
	return codec.LoadFloat32FromBuf(br, opts...)
}

// LoadFloat64FromBuf deserializes one float64 prefix and advances br. The result
// may borrow br.Buf; keep it alive and unchanged while using it.
func LoadFloat64FromBuf(br *array.BufReader, opts ...ReadOptions) (EncodedArray[float64], error) {
	return codec.LoadFloat64FromBuf(br, opts...)
}

// LoadStringsFromBuf deserializes one string prefix and advances br. The result
// may borrow br.Buf; keep it alive and unchanged while using it.
func LoadStringsFromBuf(br *array.BufReader, opts ...ReadOptions) (EncodedArray[string], error) {
	return codec.LoadStringsFromBuf(br, opts...)
}

// Decompress materializes encoded, rejecting outputs larger than 64 MiB.
func Decompress[T Integer | Float | String](encoded EncodedArray[T]) ([]T, error) {
	return codec.Decompress(encoded)
}

// DecompressTrusted materializes encoded without the conservative decoded-byte
// limit. Use it only when a higher-level memory budget governs the result.
func DecompressTrusted[T Integer | Float | String](encoded EncodedArray[T]) ([]T, error) {
	return codec.DecompressTrusted(encoded)
}
