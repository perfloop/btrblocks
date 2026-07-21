package compress

import (
	"errors"

	"github.com/axiomhq/btrblocks/array"
	"github.com/axiomhq/btrblocks/codec"
)

// CodecType identifies a codec considered or selected by the planner.
type CodecType = codec.CodecType

// EncodedArray is the typed result of planning and building a codec tree.
type EncodedArray[T array.Integer | array.Float | array.String] = codec.EncodedArray[T]

const (
	DefaultMaxBuildBytes = codec.DefaultMaxBuildBytes

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

	headerSize = codec.HeaderSize
)

var errOffsetOutOfRange = errors.New("offset out of range")

// These aliases preserve codec's sentinel identities. Returned errors
// may wrap them; callers should classify failures with errors.Is.
var (
	ErrDepthExhausted        = codec.ErrDepthExhausted
	ErrDataEmpty             = codec.ErrDataEmpty
	ErrValueNotConstant      = codec.ErrValueNotConstant
	ErrNotArithmeticSequence = codec.ErrNotArithmeticSequence
	ErrALPHighPatchRatio     = codec.ErrALPHighPatchRatio
	ErrALPRDHighPatchRatio   = codec.ErrALPRDHighPatchRatio
	ErrMaterializationLimit  = codec.ErrMaterializationLimit
)
