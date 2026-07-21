// Package array provides materialized typed columnar arrays.
//
// Primitives owns fixed-width numeric values. Strings owns a contiguous byte
// buffer and an offset array. NewPrimitives copies its input; the Unsafe form
// borrows it. Read operations may borrow the input buffer as well.
//
// Validity is native array metadata; null rows retain a physical value that
// callers must ignore. ArrayCore is the small read-only interface used by
// planners and codecs. Array adds serialization and slicing for materialized
// values.
package array
