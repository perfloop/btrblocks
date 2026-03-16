package btrblocks

import (
	"math"

	"github.com/axiomhq/btrblocks/array"
)

type (
	Integer         = array.Integer
	SignedInteger   = array.SignedInteger
	UnsignedInteger = array.UnsignedInteger
	Float           = array.Float
	String          = array.String
	PrimitiveType   = array.PrimitiveType
)

type PType = array.PType

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

func pTypeForType[T Integer | Float | String]() PType {
	return array.PTypeForType[T]()
}

func cmpFloats[T Float](a, b T) bool {
	switch av := any(a).(type) {
	case float32:
		return math.Float32bits(av) == math.Float32bits(any(b).(float32))
	case float64:
		return math.Float64bits(av) == math.Float64bits(any(b).(float64))
	default:
		return false
	}
}

func cmpIntegers[T Integer](a, b T) bool { return a == b }
func cmpStrings[T String](a, b T) bool   { return a == b }

type cmpFn[T Integer | Float | String] func(a, b T) bool
