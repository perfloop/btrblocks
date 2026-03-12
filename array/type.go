package array

type PType uint8

const (
	PTypeUnknown PType = iota
	PTypeInt8
	PTypeInt16
	PTypeInt32
	PTypeInt64
	PTypeUint8
	PTypeUint16
	PTypeUint32
	PTypeUint64
	PTypeFloat32
	PTypeFloat64
	PTypeString
)

func (p PType) String() string {
	switch p {
	case PTypeInt8:
		return "int8"
	case PTypeInt16:
		return "int16"
	case PTypeInt32:
		return "int32"
	case PTypeInt64:
		return "int64"
	case PTypeUint8:
		return "uint8"
	case PTypeUint16:
		return "uint16"
	case PTypeUint32:
		return "uint32"
	case PTypeUint64:
		return "uint64"
	case PTypeFloat32:
		return "float32"
	case PTypeFloat64:
		return "float64"
	case PTypeString:
		return "string"
	default:
		return "unknown"
	}
}

func (p PType) IsInteger() bool {
	return p == PTypeInt8 || p == PTypeInt16 || p == PTypeInt32 || p == PTypeInt64 || p == PTypeUint8 || p == PTypeUint16 || p == PTypeUint32 || p == PTypeUint64
}

func (p PType) IsFloat() bool {
	return p == PTypeFloat32 || p == PTypeFloat64
}

func (p PType) IsString() bool {
	return p == PTypeString
}

func (p PType) IsPrimitive() bool {
	return p.IsInteger() || p.IsFloat() || p.IsString()
}

type SignedInteger interface {
	~int8 | ~int16 | ~int32 | ~int64
}

type UnsignedInteger interface {
	~uint8 | ~uint16 | ~uint32 | ~uint64
}

type Integer interface {
	SignedInteger | UnsignedInteger
}

type Float interface {
	~float32 | ~float64
}

type PrimitiveType interface {
	Integer | Float
}

type String interface {
	~string
}

func pTypeForType[T Integer | Float | String]() PType {
	var t T
	switch any(t).(type) {
	case int8:
		return PTypeInt8
	case int16:
		return PTypeInt16
	case int32:
		return PTypeInt32
	case int64:
		return PTypeInt64
	case uint8:
		return PTypeUint8
	case uint16:
		return PTypeUint16
	case uint32:
		return PTypeUint32
	case uint64:
		return PTypeUint64
	case float32:
		return PTypeFloat32
	case float64:
		return PTypeFloat64
	case string:
		return PTypeString
	default:
		return PTypeUnknown
	}
}
