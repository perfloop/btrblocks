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
)

type PType = array.PType
type ReadOptions = array.ReadOptions

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

type CodeType uint8

const (
	CodecTypeUnknown CodeType = iota
	CodecTypeConst
	CodecTypeRaw
	CodecTypeDict
	CodecTypeRunEnd
	CodecTypeZigZag
	CodecTypeBitpack
	CodecTypeFor
	_ // reserved for removed sparse encoding kind
	CodecTypeSequence
	CodecTypeALP
	CodecTypeFSST
	CodecTypeALPRD
)

func (k CodeType) String() string {
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
	case CodecTypeSequence:
		return "sequence"
	case CodecTypeALP:
		return "alp"
	case CodecTypeFSST:
		return "fsst"
	case CodecTypeALPRD:
		return "alprd"
	default:
		return "unknown"
	}
}

// Options configures compression behavior. The zero value enables all schemes
// with the default cascade depth.
//
// Use the fluent With* methods to customise, or start from EmptySchemes() to
// build an allowlist:
//
//	// Exclude specific schemes (blocklist):
//	opts := Options{}.WithExcludeFloat(CodecTypeALP)
//
//	// Only allow specific schemes (allowlist):
//	opts := EmptySchemes().WithIncludeInteger(CodecTypeBitpack, CodecTypeFor)
type Options struct {
	MaxDepth int
	excludes plannerExcludes
}

// EmptySchemes returns Options with every scheme excluded. Call WithInclude*
// to selectively enable schemes — mirrors BtrBlocksCompressorBuilder::empty()
// in the Rust reference.
func EmptySchemes() Options {
	return Options{excludes: plannerExcludes{
		integers: ^kindSet(0),
		floats:   ^kindSet(0),
		strings:  ^kindSet(0),
	}}
}

// WithMaxDepth sets the maximum cascade depth.
func (o Options) WithMaxDepth(d int) Options {
	o.MaxDepth = d
	return o
}

// WithExcludeInteger removes the given integer schemes from the enabled set.
func (o Options) WithExcludeInteger(kinds ...CodeType) Options {
	o.excludes.integers = o.excludes.integers.with(kinds...)
	return o
}

// WithIncludeInteger adds the given integer schemes back into the enabled set.
func (o Options) WithIncludeInteger(kinds ...CodeType) Options {
	for _, k := range kinds {
		o.excludes.integers &^= 1 << k
	}
	return o
}

// WithExcludeFloat removes the given float schemes from the enabled set.
func (o Options) WithExcludeFloat(kinds ...CodeType) Options {
	o.excludes.floats = o.excludes.floats.with(kinds...)
	return o
}

// WithIncludeFloat adds the given float schemes back into the enabled set.
func (o Options) WithIncludeFloat(kinds ...CodeType) Options {
	for _, k := range kinds {
		o.excludes.floats &^= 1 << k
	}
	return o
}

// WithExcludeString removes the given string schemes from the enabled set.
func (o Options) WithExcludeString(kinds ...CodeType) Options {
	o.excludes.strings = o.excludes.strings.with(kinds...)
	return o
}

// WithIncludeString adds the given string schemes back into the enabled set.
func (o Options) WithIncludeString(kinds ...CodeType) Options {
	for _, k := range kinds {
		o.excludes.strings &^= 1 << k
	}
	return o
}

const defaultMaxDepth = 3

func normalizeOptions(opts Options) Options {
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = defaultMaxDepth
	}
	return opts
}

// floatBits returns the IEEE 754 bit pattern of a float as uint64.
// Used as a map key for bit-exact float equality (NaN-safe, signed-zero-aware).
func floatBits[T Float](value T) uint64 {
	switch v := any(value).(type) {
	case float32:
		return uint64(math.Float32bits(v))
	case float64:
		return math.Float64bits(v)
	default:
		return 0
	}
}

// cmpFloatBits compares floats by bit pattern. Two NaNs with the same bits
// are equal; +0 and -0 are distinct. Used by const and dict detection.
func cmpFloatBits[T Float](a, b T) bool {
	return floatBits(a) == floatBits(b)
}

// cmpFloatEq compares floats by value. NaN != NaN, so consecutive NaNs break
// runs. +0 == -0. Used by run-length detection.
func cmpFloatEq[T Float](a, b T) bool { return a == b }

type cmpFn[T Integer | Float | String] func(T, T) bool

func cmpIntegers[T Integer](a, b T) bool { return a == b }
func cmpStrings[T String](a, b T) bool   { return a == b }
