package array

import "io"

// Array is the common interface for columnar array types (primitives and strings).
// All arrays have a fixed PType, support O(1) ValueAt by index, and can be serialized via WriteTo.
type Array[T Integer | Float | String] interface {
	io.WriterTo
	// ValueAt returns the value at the given index. Panics if offset >= Length().
	ValueAt(offset uint64) T
	// BinarySize is the total size in bytes when written (header + body).
	BinarySize() uint64
	// Length is the number of elements in the array.
	Length() uint64
	// PType identifies the element type for this array.
	PType() PType
}
