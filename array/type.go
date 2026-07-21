package array

import "unsafe"

// PType identifies the physical element type of an array (int8, uint32, string, etc.).
// PType values are persisted to disk (array headers, logical value tags,
// Dynamic shared-overflow entries); the enum is append-only: never reorder,
// insert, or remove entries.
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

// ByteWidth returns the byte width of a fixed-width physical type.
// It returns 0 for unknown or variable-width types such as string.
func (p PType) ByteWidth() int {
	switch p {
	case PTypeInt8, PTypeUint8:
		return 1
	case PTypeInt16, PTypeUint16:
		return 2
	case PTypeInt32, PTypeUint32, PTypeFloat32:
		return 4
	case PTypeInt64, PTypeUint64, PTypeFloat64:
		return 8
	default:
		return 0
	}
}

// SignedInteger is the set of signed integer types supported as primitive elements.
type SignedInteger interface {
	~int8 | ~int16 | ~int32 | ~int64
}

// UnsignedInteger is the set of unsigned integer types supported as primitive elements and used for string offset arrays.
type UnsignedInteger interface {
	~uint8 | ~uint16 | ~uint32 | ~uint64
}

// Integer is the union of signed and unsigned integer types.
type Integer interface {
	SignedInteger | UnsignedInteger
}

// Float is the set of floating-point types supported as primitive elements.
type Float interface {
	~float32 | ~float64
}

// PrimitiveType is any numeric type that can be stored in a Primitives array.
type PrimitiveType interface {
	Integer | Float
}

// String is a type constraint for string arrays (currently just string).
type String interface {
	~string
}

// CmpIntegers reports whether a and b are equal integer values.
func CmpIntegers[T Integer](a, b T) bool { return a == b }

// CmpStrings reports whether a and b are equal string values.
func CmpStrings[T String](a, b T) bool { return a == b }

// CmpFloatBits reports whether a and b have identical IEEE-754 bit patterns,
// so NaN and signed-zero distinctions survive bit-exact planning decisions.
func CmpFloatBits[T Float](a, b T) bool { return FloatBits(a) == FloatBits(b) }

// FloatBits returns the IEEE-754 bit pattern of value, for bit-exact float
// hashing and comparison.
func FloatBits[T Float](value T) uint64 {
	switch unsafe.Sizeof(value) {
	case 4:
		return uint64(*(*uint32)(unsafe.Pointer(&value)))
	case 8:
		return *(*uint64)(unsafe.Pointer(&value))
	}
	panic("array: unsupported float width")
}

// PTypeOfPrimitive returns the physical type of a numeric T without erasing T
// into an interface. Generic arithmetic distinguishes float, signed integer,
// and unsigned integer families; unsafe.Sizeof selects the width.
func PTypeOfPrimitive[T PrimitiveType]() PType {
	var minusOne T
	minusOne--
	isFloat := T(1)/T(2) != T(0)
	isSigned := minusOne < T(0)
	switch unsafe.Sizeof(T(0)) {
	case 1:
		if isSigned {
			return PTypeInt8
		}
		return PTypeUint8
	case 2:
		if isSigned {
			return PTypeInt16
		}
		return PTypeUint16
	case 4:
		if isFloat {
			return PTypeFloat32
		}
		if isSigned {
			return PTypeInt32
		}
		return PTypeUint32
	case 8:
		if isFloat {
			return PTypeFloat64
		}
		if isSigned {
			return PTypeInt64
		}
		return PTypeUint64
	}
	return PTypeUnknown
}
