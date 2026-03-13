package btrblocks

import (
	"errors"
	"io"

	"github.com/axiomhq/btrblocks/array"
)

var (
	errOffsetOutOfRange = errors.New("offset out of range")
	errDepthExhausted   = errors.New("depth exhausted")
	errDataEmpty        = errors.New("data is empty")
)

type CodecType uint8

const (
	CodecTypeUnknown CodecType = iota
	CodecTypeConst
	CodecTypeRaw
	CodecTypeDict
	CodecTypeRunend
	CodecTypeZigzag
	CodecTypeBitpacking
)

// Scheme is a untyped structural interface
type Scheme interface {
	Children() []Scheme
}

// Codec is a typed access interface
type Codec[T Integer | Float | String] interface {
	Scheme
	io.WriterTo
	ValueAt(offset uint64) (T, error)
	BinarySize() uint64
	Length() uint64
	PType() PType
}

type codecBuilder[T Integer | Float | String] func(array.Array[T], int) (Codec[T], error)
