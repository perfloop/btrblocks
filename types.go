package btrblocks

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
	case int8:
		return PTypeInteger
	case int16:
		return PTypeInteger
	case int32:
		return PTypeInteger
	case int64:
		return PTypeInteger
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
