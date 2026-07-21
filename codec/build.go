package codec

import (
	"fmt"
	"math/bits"
	"unsafe"

	"github.com/axiomhq/btrblocks/array"
)

// HeaderSize is the serialized size of every codec node header.
const HeaderSize = headerSize

// DefaultMaxBuildBytes is the default maximum size of one temporary
// materialization created while building a codec node. Lookup tables charge a
// conservative per-entry estimate against it.
const DefaultMaxBuildBytes = array.DefaultMaxBuildBytes

type buildBudget struct {
	maxBytes uint64
}

func (b buildBudget) check(count, width uint64, name string) error {
	if width != 0 && count > b.maxBytes/width {
		return fmt.Errorf("%w: %s needs %d values of width %d, limit %d bytes", ErrMaterializationLimit, name, count, width, b.maxBytes)
	}
	if count > uint64(^uint(0)>>1) {
		return fmt.Errorf("%w: %s length %d overflows int", ErrMaterializationLimit, name, count)
	}
	return nil
}

func checkBuildSlice[T any](budget buildBudget, count uint64, name string) error {
	var value T
	return budget.check(count, uint64(unsafe.Sizeof(value)), name)
}

func checkBuildMapEntries[K any](budget buildBudget, entries uint64, name string) error {
	var key K
	// Go maps and intmap entries need hash/control storage in addition to the
	// key and ordinal. Sixty-four bytes per live key is a conservative bound
	// for the key types supported by codec builders.
	width := max(uint64(64), uint64(unsafe.Sizeof(key))+16)
	return budget.check(entries, width, name)
}

func buildMapEntryLimit[K any](budget buildBudget) uint64 {
	var key K
	width := max(uint64(64), uint64(unsafe.Sizeof(key))+16)
	return budget.maxBytes / width
}

func cappedBuildCapacity[T any](budget buildBudget, wanted uint64) uint64 {
	var value T
	width := uint64(unsafe.Sizeof(value))
	if width == 0 {
		return wanted
	}
	return min(wanted, budget.maxBytes/width)
}

func makeBuildSlice[T any](budget buildBudget, length, capacity uint64, name string) ([]T, error) {
	if capacity < length {
		return nil, fmt.Errorf("codec: %s capacity %d is smaller than length %d", name, capacity, length)
	}
	if err := checkBuildSlice[T](budget, capacity, name); err != nil {
		return nil, err
	}
	return make([]T, length, capacity), nil
}

func appendBuildValue[T any](values []T, value T, budget buildBudget, name string) ([]T, error) {
	if len(values) < cap(values) {
		return append(values, value), nil
	}
	var zero T
	width := uint64(unsafe.Sizeof(zero))
	maxCount := budget.maxBytes
	if width != 0 {
		maxCount /= width
	}
	if uint64(len(values)) >= maxCount || uint64(len(values)) >= uint64(^uint(0)>>1) {
		return nil, fmt.Errorf("%w: %s exceeds %d bytes", ErrMaterializationLimit, name, budget.maxBytes)
	}
	newCapacity := max(uint64(8), uint64(cap(values))+uint64(cap(values))/2)
	if newCapacity <= uint64(len(values)) {
		newCapacity = uint64(len(values)) + 1
	}
	newCapacity = min(newCapacity, maxCount)
	next, err := makeBuildSlice[T](budget, uint64(len(values)), newCapacity, name)
	if err != nil {
		return nil, err
	}
	copy(next, values)
	return append(next, value), nil
}

func appendBuildSlice[T any](values, added []T, budget buildBudget, name string) ([]T, error) {
	if len(added) == 0 {
		return values, nil
	}
	if uint64(len(added)) > ^uint64(0)-uint64(len(values)) {
		return nil, fmt.Errorf("%w: %s length overflows", ErrMaterializationLimit, name)
	}
	required := uint64(len(values)) + uint64(len(added))
	if err := checkBuildSlice[T](budget, required, name); err != nil {
		return nil, err
	}
	if required <= uint64(cap(values)) {
		return append(values, added...), nil
	}
	newCapacity := max(required, uint64(cap(values))+uint64(cap(values))/2)
	newCapacity = min(newCapacity, cappedBuildCapacity[T](budget, newCapacity))
	next, err := makeBuildSlice[T](budget, uint64(len(values)), newCapacity, name)
	if err != nil {
		return nil, err
	}
	copy(next, values)
	return append(next, added...), nil
}

// ChildBuilder compresses one structural child of a codec node. It must return
// a non-nil, non-nullable encoding with the same length and bit-exact values as
// its input. Encode functions enforce every part of that contract that is O(1);
// value equality is the builder's responsibility.
type ChildBuilder[T Integer | Float | String] func(array.ArrayCore[T]) (EncodedArray[T], error)

// adoptChild validates a ChildBuilder result before it is spliced into a node.
// The checks are O(1) by design: a nil, mis-sized, or nullable child corrupts
// the parent, while verifying values would decompress every child on every
// build. srcLen is the length of the input the child was built from.
func adoptChild[T Integer | Float | String](srcLen uint64, child EncodedArray[T], err error) (EncodedArray[T], error) {
	if err != nil {
		return nil, err
	}
	if child == nil {
		return nil, fmt.Errorf("codec: builder returned a nil child")
	}
	if child.Length() != srcLen {
		return nil, fmt.Errorf("codec: child length = %d, want %d", child.Length(), srcLen)
	}
	if err := requireNonNullable(child, "adopted"); err != nil {
		return nil, err
	}
	return child, nil
}

// maxBuildBytes collapses a variadic BuildOptions tail to the single option the
// builders honour. Extras are rejected rather than silently dropped: accepting
// them now would make it a breaking change to give them meaning later.
func maxBuildBytes(opts []BuildOptions) (uint64, error) {
	if len(opts) > 1 {
		return 0, fmt.Errorf("codec: at most one BuildOptions is allowed, got %d", len(opts))
	}
	if len(opts) == 1 && opts[0].MaxBytes != 0 {
		return opts[0].MaxBytes, nil
	}
	return DefaultMaxBuildBytes, nil
}

func validateBuildAllocation[T Integer | Float | String](length, maxBytes uint64) error {
	return checkBuildSlice[T](buildBudget{maxBytes: maxBytes}, length, "build source")
}

func validateBuildSource[T Integer | Float | String](source array.ArrayCore[T], opts []BuildOptions) (uint64, error) {
	if source == nil {
		return 0, fmt.Errorf("codec: nil build source")
	}
	maxBytes, err := maxBuildBytes(opts)
	if err != nil {
		return 0, err
	}
	if err := validateBuildAllocation[T](source.Length(), maxBytes); err != nil {
		return 0, err
	}
	if source.NullCount() != 0 {
		return 0, fmt.Errorf("codec: build source contains %d nulls", source.NullCount())
	}
	return maxBytes, nil
}

// UnsignedChildBuilder compresses an unsigned structural child using the width
// selected by the codec. Calls occur once per structural child, never per row.
type UnsignedChildBuilder interface {
	BuildUint8(array.ArrayCore[uint8]) (EncodedArray[uint8], error)
	BuildUint16(array.ArrayCore[uint16]) (EncodedArray[uint16], error)
	BuildUint32(array.ArrayCore[uint32]) (EncodedArray[uint32], error)
	BuildUint64(array.ArrayCore[uint64]) (EncodedArray[uint64], error)
}

// UnsignedChildFuncs adapts per-width closures to UnsignedChildBuilder so a
// caller can supply one without declaring a type. A nil field is reported as
// ErrBuilderRequired when the codec selects that width.
type UnsignedChildFuncs struct {
	Uint8  ChildBuilder[uint8]
	Uint16 ChildBuilder[uint16]
	Uint32 ChildBuilder[uint32]
	Uint64 ChildBuilder[uint64]
}

func (f UnsignedChildFuncs) BuildUint8(source array.ArrayCore[uint8]) (EncodedArray[uint8], error) {
	return callChild(f.Uint8, source)
}

func (f UnsignedChildFuncs) BuildUint16(source array.ArrayCore[uint16]) (EncodedArray[uint16], error) {
	return callChild(f.Uint16, source)
}

func (f UnsignedChildFuncs) BuildUint32(source array.ArrayCore[uint32]) (EncodedArray[uint32], error) {
	return callChild(f.Uint32, source)
}

func (f UnsignedChildFuncs) BuildUint64(source array.ArrayCore[uint64]) (EncodedArray[uint64], error) {
	return callChild(f.Uint64, source)
}

func callChild[T Integer | Float | String](build ChildBuilder[T], source array.ArrayCore[T]) (EncodedArray[T], error) {
	if build == nil {
		return nil, ErrBuilderRequired
	}
	return build(source)
}

// DictionaryChildBuilder compresses dictionary values and ordinal children.
type DictionaryChildBuilder[T Integer | Float | String] interface {
	UnsignedChildBuilder
	BuildValues(array.ArrayCore[T]) (EncodedArray[T], error)
}

// SparseChildBuilder compresses sparse fills, values, and index children.
type SparseChildBuilder[T Integer | Float | String] interface {
	UnsignedChildBuilder
	BuildFill(array.ArrayCore[T]) (EncodedArray[T], error)
	BuildValues(array.ArrayCore[T]) (EncodedArray[T], error)
}

// ALPChildBuilder compresses ALP's encoded integer and patch-index children.
type ALPChildBuilder interface {
	UnsignedChildBuilder
	BuildInt32(array.ArrayCore[int32]) (EncodedArray[int32], error)
	BuildInt64(array.ArrayCore[int64]) (EncodedArray[int64], error)
}

// EncodeRaw wraps a materialized array in a raw codec node.
func EncodeRaw[T Integer | Float | String](arr array.Array[T]) (EncodedArray[T], error) {
	if arr == nil {
		return nil, fmt.Errorf("codec: nil raw array")
	}
	return encodeRaw(arr), nil
}

// EncodeConstInteger builds a constant integer node.
func EncodeConstInteger[T Integer](arr array.ArrayCore[T], opts ...BuildOptions) (EncodedArray[T], error) {
	if _, err := validateBuildSource(arr, opts); err != nil {
		return nil, err
	}
	return encodeConstInteger(arr)
}

// EncodeConstFloat builds a bitwise-constant float node.
func EncodeConstFloat[T Float](arr array.ArrayCore[T], opts ...BuildOptions) (EncodedArray[T], error) {
	if _, err := validateBuildSource(arr, opts); err != nil {
		return nil, err
	}
	return encodeConstFloat(arr)
}

// EncodeConstString builds a constant string node.
func EncodeConstString(arr array.ArrayCore[string], opts ...BuildOptions) (EncodedArray[string], error) {
	if _, err := validateBuildSource(arr, opts); err != nil {
		return nil, err
	}
	return encodeConstString(arr)
}

// NewConstArray constructs a constant node from its one-element body.
func NewConstArray[T Integer | Float | String](length uint64, body array.Array[T]) (EncodedArray[T], error) {
	if length == 0 {
		return nil, ErrDataEmpty
	}
	if body == nil || body.Length() != 1 {
		return nil, fmt.Errorf("codec: const body length must be one")
	}
	if err := requireNonNullable(body, "const body"); err != nil {
		return nil, err
	}
	return &constArray[T]{denseRows: denseRows(length), body: body}, nil
}

// EncodeSequence builds an arithmetic-sequence node.
func EncodeSequence[T Integer](arr array.ArrayCore[T], opts ...BuildOptions) (EncodedArray[T], error) {
	if _, err := validateBuildSource(arr, opts); err != nil {
		return nil, err
	}
	return encodeSequence(arr)
}

// EncodeFoR builds a frame-of-reference node.
func EncodeFoR[T Integer](arr array.ArrayCore[T], opts ...BuildOptions) (EncodedArray[T], error) {
	maxBytes, err := validateBuildSource(arr, opts)
	if err != nil {
		return nil, err
	}
	return encodeFoR(arr, buildBudget{maxBytes: maxBytes})
}

// EncodeDelta builds a delta node and delegates residual compression.
func EncodeDelta[T Integer](arr array.ArrayCore[T], buildChild ChildBuilder[T], opts ...BuildOptions) (EncodedArray[T], error) {
	if _, err := validateBuildSource(arr, opts); err != nil {
		return nil, err
	}
	if buildChild == nil {
		return nil, ErrBuilderRequired
	}
	return encodeDelta(arr, buildChild)
}

// EncodeIntegerDict builds an integer dictionary node and its children.
func EncodeIntegerDict[T Integer](arr array.ArrayCore[T], children DictionaryChildBuilder[T], opts ...BuildOptions) (EncodedArray[T], error) {
	maxBytes, err := validateBuildSource(arr, opts)
	if err != nil {
		return nil, err
	}
	if children == nil {
		return nil, ErrBuilderRequired
	}
	return encodeIntegerDict(arr, nil, children, buildBudget{maxBytes: maxBytes})
}

// EncodeFloat32Dict builds a float32 dictionary node and its children.
func EncodeFloat32Dict(arr array.ArrayCore[float32], expectedDistinct uint64, children DictionaryChildBuilder[float32], opts ...BuildOptions) (EncodedArray[float32], error) {
	maxBytes, err := validateBuildSource(arr, opts)
	if err != nil {
		return nil, err
	}
	if children == nil {
		return nil, ErrBuilderRequired
	}
	return encodeFloat32Dict(arr, nil, expectedDistinct, children, buildBudget{maxBytes: maxBytes})
}

// EncodeFloat64Dict builds a float64 dictionary node and its children.
func EncodeFloat64Dict(arr array.ArrayCore[float64], expectedDistinct uint64, children DictionaryChildBuilder[float64], opts ...BuildOptions) (EncodedArray[float64], error) {
	maxBytes, err := validateBuildSource(arr, opts)
	if err != nil {
		return nil, err
	}
	if children == nil {
		return nil, ErrBuilderRequired
	}
	return encodeFloat64Dict(arr, nil, expectedDistinct, children, buildBudget{maxBytes: maxBytes})
}

// EncodeStringDict builds a string dictionary node and its children.
func EncodeStringDict(arr array.ArrayCore[string], children DictionaryChildBuilder[string], opts ...BuildOptions) (EncodedArray[string], error) {
	maxBytes, err := validateBuildSource(arr, opts)
	if err != nil {
		return nil, err
	}
	if children == nil {
		return nil, ErrBuilderRequired
	}
	return encodeStringDict(arr, children, buildBudget{maxBytes: maxBytes})
}

// EncodePrimitiveRunEndAs builds a bit-exact numeric run-end node using index
// type I. It rejects arrays whose final boundary I cannot represent.
func EncodePrimitiveRunEndAs[V Integer | Float, I UnsignedInteger](arr array.ArrayCore[V], buildRuns ChildBuilder[V], buildEnds ChildBuilder[I], opts ...BuildOptions) (EncodedArray[V], error) {
	maxBytes, err := validateBuildSource(arr, opts)
	if err != nil {
		return nil, err
	}
	if buildRuns == nil || buildEnds == nil {
		return nil, ErrBuilderRequired
	}
	if arr.Length() > 0 && arr.Length()-1 > uint64(^I(0)) {
		return nil, fmt.Errorf("codec: run-end index type cannot represent offset %d", arr.Length()-1)
	}
	return encodePrimitiveRunEndAs(arr, cmpBitExact[V](), buildRuns, buildEnds, buildBudget{maxBytes: maxBytes})
}

// EncodeStringRunEndAs builds a string run-end node using index type I. It
// rejects arrays whose final boundary I cannot represent.
func EncodeStringRunEndAs[I UnsignedInteger](arr array.ArrayCore[string], buildRuns ChildBuilder[string], buildEnds ChildBuilder[I], opts ...BuildOptions) (EncodedArray[string], error) {
	maxBytes, err := validateBuildSource(arr, opts)
	if err != nil {
		return nil, err
	}
	if buildRuns == nil || buildEnds == nil {
		return nil, ErrBuilderRequired
	}
	if arr.Length() > 0 && arr.Length()-1 > uint64(^I(0)) {
		return nil, fmt.Errorf("codec: run-end index type cannot represent offset %d", arr.Length()-1)
	}
	return encodeStringRunEndAs(arr, array.CmpStrings[string], buildRuns, buildEnds, buildBudget{maxBytes: maxBytes})
}

// EncodeIntegerSparse builds an integer sparse node and its children.
func EncodeIntegerSparse[T Integer](arr array.ArrayCore[T], children SparseChildBuilder[T], opts ...BuildOptions) (EncodedArray[T], error) {
	maxBytes, err := validateBuildSource(arr, opts)
	if err != nil {
		return nil, err
	}
	if children == nil {
		return nil, ErrBuilderRequired
	}
	return encodeIntegerSparse(arr, children, buildBudget{maxBytes: maxBytes})
}

// EncodeIntegerSparseWithFill builds an integer sparse node with zero fill.
func EncodeIntegerSparseWithFill[T Integer](arr array.ArrayCore[T], nullCount uint64, children SparseChildBuilder[T], opts ...BuildOptions) (EncodedArray[T], error) {
	maxBytes, err := validateBuildSource(arr, opts)
	if err != nil {
		return nil, err
	}
	if children == nil {
		return nil, ErrBuilderRequired
	}
	return encodeIntegerSparseWithFill(arr, nullCount, children, buildBudget{maxBytes: maxBytes})
}

// EncodeFloatSparse builds a bitwise float sparse node and its children.
func EncodeFloatSparse[T Float](arr array.ArrayCore[T], children SparseChildBuilder[T], opts ...BuildOptions) (EncodedArray[T], error) {
	maxBytes, err := validateBuildSource(arr, opts)
	if err != nil {
		return nil, err
	}
	if children == nil {
		return nil, ErrBuilderRequired
	}
	return encodeFloatSparse(arr, children, buildBudget{maxBytes: maxBytes})
}

// EncodeFloatSparseWithFill builds a float sparse node with positive-zero fill.
func EncodeFloatSparseWithFill[T Float](arr array.ArrayCore[T], nullCount uint64, children SparseChildBuilder[T], opts ...BuildOptions) (EncodedArray[T], error) {
	maxBytes, err := validateBuildSource(arr, opts)
	if err != nil {
		return nil, err
	}
	if children == nil {
		return nil, ErrBuilderRequired
	}
	return encodeFloatSparseWithFill(arr, nullCount, children, buildBudget{maxBytes: maxBytes})
}

// EncodeStringSparse builds a string sparse node and its children.
func EncodeStringSparse(arr array.ArrayCore[string], children SparseChildBuilder[string], opts ...BuildOptions) (EncodedArray[string], error) {
	maxBytes, err := validateBuildSource(arr, opts)
	if err != nil {
		return nil, err
	}
	if children == nil {
		return nil, ErrBuilderRequired
	}
	return encodeStringSparse(arr, children, buildBudget{maxBytes: maxBytes})
}

// EncodeStringSparseWithFill builds a string sparse node with empty fill.
func EncodeStringSparseWithFill(arr array.ArrayCore[string], nullCount uint64, children SparseChildBuilder[string], opts ...BuildOptions) (EncodedArray[string], error) {
	maxBytes, err := validateBuildSource(arr, opts)
	if err != nil {
		return nil, err
	}
	if children == nil {
		return nil, ErrBuilderRequired
	}
	return encodeStringSparseWithFill(arr, nullCount, children, buildBudget{maxBytes: maxBytes})
}

// ZigZagEncodedType returns the narrowest unsigned child type for arr.
func ZigZagEncodedType[T SignedInteger](arr array.ArrayCore[T], opts ...BuildOptions) (PType, error) {
	if _, err := validateBuildSource(arr, opts); err != nil {
		return PTypeUnknown, err
	}
	return zigZagEncodedType(arr), nil
}

// EncodeZigZagAs builds a zig-zag node with unsigned child type U.
func EncodeZigZagAs[T SignedInteger, U UnsignedInteger](arr array.ArrayCore[T], buildChild ChildBuilder[U], opts ...BuildOptions) (EncodedArray[T], error) {
	if _, err := validateBuildSource(arr, opts); err != nil {
		return nil, err
	}
	if buildChild == nil {
		return nil, ErrBuilderRequired
	}
	return encodeZigZagAs(arr, buildChild)
}

// EncodeBitpack builds a bit-packed integer node.
func EncodeBitpack[T Integer](arr array.ArrayCore[T], opts ...BuildOptions) (EncodedArray[T], error) {
	maxBytes, err := validateBuildSource(arr, opts)
	if err != nil {
		return nil, err
	}
	return encodeBitpack(arr, buildBudget{maxBytes: maxBytes})
}

// EncodeALP32 builds an ALP float32 node and its children.
func EncodeALP32(arr array.ArrayCore[float32], children ALPChildBuilder, opts ...BuildOptions) (EncodedArray[float32], error) {
	maxBytes, err := validateBuildSource(arr, opts)
	if err != nil {
		return nil, err
	}
	if children == nil {
		return nil, ErrBuilderRequired
	}
	return encodeALP32(arr, children, buildBudget{maxBytes: maxBytes})
}

// EncodeALP64 builds an ALP float64 node and its children.
func EncodeALP64(arr array.ArrayCore[float64], children ALPChildBuilder, opts ...BuildOptions) (EncodedArray[float64], error) {
	maxBytes, err := validateBuildSource(arr, opts)
	if err != nil {
		return nil, err
	}
	if children == nil {
		return nil, ErrBuilderRequired
	}
	return encodeALP64(arr, children, buildBudget{maxBytes: maxBytes})
}

// EncodeALPRD32 builds an ALP-RD float32 node and its children.
func EncodeALPRD32(arr array.ArrayCore[float32], children UnsignedChildBuilder, opts ...BuildOptions) (EncodedArray[float32], error) {
	maxBytes, err := validateBuildSource(arr, opts)
	if err != nil {
		return nil, err
	}
	if children == nil {
		return nil, ErrBuilderRequired
	}
	return encodeALPRD32(arr, children, buildBudget{maxBytes: maxBytes})
}

// EncodeALPRD64 builds an ALP-RD float64 node and its children.
func EncodeALPRD64(arr array.ArrayCore[float64], children UnsignedChildBuilder, opts ...BuildOptions) (EncodedArray[float64], error) {
	maxBytes, err := validateBuildSource(arr, opts)
	if err != nil {
		return nil, err
	}
	if children == nil {
		return nil, ErrBuilderRequired
	}
	return encodeALPRD64(arr, children, buildBudget{maxBytes: maxBytes})
}

// EncodeFSST builds an FSST string node and its offset/length children.
func EncodeFSST(arr array.ArrayCore[string], offsetsChildren, lengthsChildren UnsignedChildBuilder, opts ...BuildOptions) (EncodedArray[string], error) {
	maxBytes, err := validateBuildSource(arr, opts)
	if err != nil {
		return nil, err
	}
	if offsetsChildren == nil || lengthsChildren == nil {
		return nil, ErrBuilderRequired
	}
	return encodeFSST(arr, offsetsChildren, lengthsChildren, buildBudget{maxBytes: maxBytes})
}

// NewNullable composes value and validity encodings into a nullable node.
//
// Validity is an arbitrary codec tree, so checking its bitmap against nullCount
// means decoding it. NewNullable materializes the whole bitmap once, one byte
// per eight rows of values, and returns ErrMaterializationLimit when that
// buffer would exceed BuildOptions.MaxBytes, or DefaultMaxBuildBytes when the
// caller passes none.
func NewNullable[T Integer | Float | String](values EncodedArray[T], validity EncodedArray[uint8], nullCount uint64, opts ...BuildOptions) (EncodedArray[T], error) {
	maxBytes, err := maxBuildBytes(opts)
	if err != nil {
		return nil, err
	}
	if values == nil || validity == nil {
		return nil, fmt.Errorf("codec: nullable children must not be nil")
	}
	if nullCount == 0 || nullCount > values.Length() {
		return nil, fmt.Errorf("codec: nullable null count = %d, want in [1, %d]", nullCount, values.Length())
	}
	if values.CodecType() == CodecTypeNullable || validity.CodecType() == CodecTypeNullable {
		return nil, fmt.Errorf("codec: nullable children must not themselves be nullable")
	}
	if err := requireNonNullable(values, "nullable values"); err != nil {
		return nil, err
	}
	if err := requireNonNullable(validity, "nullable validity"); err != nil {
		return nil, err
	}
	wantBytes := validityByteLength(values.Length())
	if validity.Length() != wantBytes {
		return nil, fmt.Errorf("codec: nullable validity length = %d, want %d", validity.Length(), wantBytes)
	}
	// One sequential decode, not a ValueAt walk: validity is an arbitrary codec
	// tree, and random access costs O(offset) in the delta codec, which would
	// make building one nullable column O((n/8)^2).
	bitmap, err := decompress(validity, maxBytes)
	if err != nil {
		return nil, fmt.Errorf("codec: nullable validity scan: %w", err)
	}
	validCount := uint64(0)
	for i, value := range bitmap {
		if uint64(i)+1 == wantBytes && values.Length()&7 != 0 {
			mask := uint8(1)<<(values.Length()&7) - 1
			if value&^mask != 0 {
				return nil, fmt.Errorf("codec: nullable validity has non-zero unused bits")
			}
		}
		validCount += uint64(bits.OnesCount8(value))
	}
	if got := values.Length() - validCount; got != nullCount {
		return nil, fmt.Errorf("codec: nullable bitmap has %d nulls, metadata says %d", got, nullCount)
	}
	return &nullableArray[T]{values: values, validity: validity, nullCount: nullCount}, nil
}

// NullableView exposes the two physical children of a nullable node.
type NullableView[T Integer | Float | String] interface {
	Values() EncodedArray[T]
	ValidityEncoding() EncodedArray[uint8]
}

func (a *nullableArray[T]) Values() EncodedArray[T]               { return a.values }
func (a *nullableArray[T]) ValidityEncoding() EncodedArray[uint8] { return a.validity }

// AsNullable returns a child view when encoded's root is nullable.
func AsNullable[T Integer | Float | String](encoded EncodedArray[T]) (NullableView[T], bool) {
	view, ok := encoded.(NullableView[T])
	return view, ok
}
