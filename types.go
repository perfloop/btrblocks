package btrblocks

import "math"

type SignedInteger interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64
}

type UnsignedInteger interface {
	~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64
}

type Float interface {
	~float32 | ~float64
}

type Integer interface {
	SignedInteger | UnsignedInteger
}

type String interface {
	~string
}

type PType uint8

const (
	PTypeUnknown PType = iota
	PTypeInteger
	PTypeUnsignedInteger
	PTypeFloat
	PTypeString
)

func pTypeForType[T Integer | Float | String]() PType {
	var t T
	switch any(t).(type) {
	case int:
		return PTypeInteger
	case int8:
		return PTypeInteger
	case int16:
		return PTypeInteger
	case int32:
		return PTypeInteger
	case int64:
		return PTypeInteger
	case uint:
		return PTypeUnsignedInteger
	case uint8:
		return PTypeUnsignedInteger
	case uint16:
		return PTypeUnsignedInteger
	case uint32:
		return PTypeUnsignedInteger
	case uint64:
		return PTypeUnsignedInteger
	case float32:
		return PTypeFloat
	case float64:
		return PTypeFloat
	case string:
		return PTypeString
	default:
		return PTypeUnknown
	}
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

func cmpIntegers[T Integer](a, b T) bool {
	switch av := any(a).(type) {
	case int:
		return av == any(b).(int)
	case int8:
		return av == any(b).(int8)
	case int16:
		return av == any(b).(int16)
	case int32:
		return av == any(b).(int32)
	case int64:
		return av == any(b).(int64)
	case uint:
		return av == any(b).(uint)
	case uint8:
		return av == any(b).(uint8)
	case uint16:
		return av == any(b).(uint16)
	case uint32:
		return av == any(b).(uint32)
	case uint64:
		return av == any(b).(uint64)
	default:
		return false
	}
}

func cmpStrings[T String](a, b T) bool {
	return a == b
}

type cmpFn[T Integer | Float | String] func(a, b T) bool
