package array

import "io"

type Array[T Integer | Float | String] interface {
	io.WriterTo
	ValueAt(offset uint64) T
	BinarySize() uint64
	Length() uint64
	PType() PType
}
