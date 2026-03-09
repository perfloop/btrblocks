package btrblocks

import (
	"errors"
	"io"
)

var ErrOffsetOutOfRange = errors.New("offset out of range")

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
