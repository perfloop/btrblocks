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
// materialization created while building a codec node.
const DefaultMaxBuildBytes uint64 = defaultMaxMaterializedBytes

// BuildOptions bounds temporary memory used by low-level builders. MaxBytes
// limits each materialized buffer; lookup tables use a conservative per-entry
// estimate. The limit is per allocation, not cumulative. Zero selects
// DefaultMaxBuildBytes.
type BuildOptions struct {
	MaxBytes uint64
}

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
// its input. Encode functions validate that contract before retaining a child.
type ChildBuilder[T Integer | Float | String] func(array.ArrayCore[T]) (EncodedArray[T], error)

func maxBuildBytes(opts []BuildOptions) uint64 {
	if len(opts) != 0 && opts[0].MaxBytes != 0 {
		return opts[0].MaxBytes
	}
	return DefaultMaxBuildBytes
}

func validateBuildAllocation[T Integer | Float | String](length, maxBytes uint64) error {
	return checkBuildSlice[T](buildBudget{maxBytes: maxBytes}, length, "build source")
}

func validateBuildSource[T Integer | Float | String](source array.ArrayCore[T], opts []BuildOptions) (uint64, error) {
	if source == nil {
		return 0, fmt.Errorf("codec: nil build source")
	}
	maxBytes := maxBuildBytes(opts)
	if err := validateBuildAllocation[T](source.Length(), maxBytes); err != nil {
		return 0, err
	}
	if source.NullCount() != 0 {
		return 0, fmt.Errorf("codec: build source contains %d nulls", source.NullCount())
	}
	return maxBytes, nil
}

func checkedChild[T Integer | Float | String](name string, build ChildBuilder[T], equal func(T, T) bool, maxBytes uint64) ChildBuilder[T] {
	return func(source array.ArrayCore[T]) (EncodedArray[T], error) {
		if err := validateBuildAllocation[T](source.Length(), maxBytes); err != nil {
			return nil, fmt.Errorf("codec: validate %s child: %w", name, err)
		}
		encoded, err := build(source)
		if err != nil {
			return nil, fmt.Errorf("codec: build %s child: %w", name, err)
		}
		if encoded == nil {
			return nil, fmt.Errorf("codec: %s builder returned a nil child", name)
		}
		if encoded.Length() != source.Length() {
			return nil, fmt.Errorf("codec: %s child length = %d, want %d", name, encoded.Length(), source.Length())
		}
		if encoded.NullCount() != 0 {
			return nil, fmt.Errorf("codec: %s child contains nulls", name)
		}
		decoded := make([]T, source.Length())
		if err := encoded.DecompressInto(decoded); err != nil {
			return nil, fmt.Errorf("codec: decompress %s child for validation: %w", name, err)
		}
		for i, value := range decoded {
			if !equal(source.ValueAt(uint64(i)), value) {
				return nil, fmt.Errorf("codec: %s child value differs at offset %d", name, i)
			}
		}
		return encoded, nil
	}
}

func cmpPrimitiveBits[T Integer | Float](a, b T) bool {
	switch unsafe.Sizeof(a) {
	case 1:
		return *(*uint8)(unsafe.Pointer(&a)) == *(*uint8)(unsafe.Pointer(&b))
	case 2:
		return *(*uint16)(unsafe.Pointer(&a)) == *(*uint16)(unsafe.Pointer(&b))
	case 4:
		return *(*uint32)(unsafe.Pointer(&a)) == *(*uint32)(unsafe.Pointer(&b))
	case 8:
		return *(*uint64)(unsafe.Pointer(&a)) == *(*uint64)(unsafe.Pointer(&b))
	default:
		return false
	}
}

type checkedUnsignedChildren struct {
	delegate UnsignedChildBuilder
	maxBytes uint64
}

func (c checkedUnsignedChildren) BuildUint8(source array.ArrayCore[uint8]) (EncodedArray[uint8], error) {
	return checkedChild("uint8", c.delegate.BuildUint8, array.CmpIntegers[uint8], c.maxBytes)(source)
}

func (c checkedUnsignedChildren) BuildUint16(source array.ArrayCore[uint16]) (EncodedArray[uint16], error) {
	return checkedChild("uint16", c.delegate.BuildUint16, array.CmpIntegers[uint16], c.maxBytes)(source)
}

func (c checkedUnsignedChildren) BuildUint32(source array.ArrayCore[uint32]) (EncodedArray[uint32], error) {
	return checkedChild("uint32", c.delegate.BuildUint32, array.CmpIntegers[uint32], c.maxBytes)(source)
}

func (c checkedUnsignedChildren) BuildUint64(source array.ArrayCore[uint64]) (EncodedArray[uint64], error) {
	return checkedChild("uint64", c.delegate.BuildUint64, array.CmpIntegers[uint64], c.maxBytes)(source)
}

// UnsignedChildBuilder compresses an unsigned structural child using the
// width selected by the codec.
type UnsignedChildBuilder interface {
	BuildUint8(array.ArrayCore[uint8]) (EncodedArray[uint8], error)
	BuildUint16(array.ArrayCore[uint16]) (EncodedArray[uint16], error)
	BuildUint32(array.ArrayCore[uint32]) (EncodedArray[uint32], error)
	BuildUint64(array.ArrayCore[uint64]) (EncodedArray[uint64], error)
}

// DictionaryChildBuilder compresses dictionary values and ordinal children.
type DictionaryChildBuilder[T Integer | Float | String] interface {
	BuildValues(array.ArrayCore[T]) (EncodedArray[T], error)
	BuildUint8(array.ArrayCore[uint8]) (EncodedArray[uint8], error)
	BuildUint16(array.ArrayCore[uint16]) (EncodedArray[uint16], error)
	BuildUint32(array.ArrayCore[uint32]) (EncodedArray[uint32], error)
	BuildUint64(array.ArrayCore[uint64]) (EncodedArray[uint64], error)
}

type checkedDictionaryChildren[T Integer | Float | String] struct {
	delegate DictionaryChildBuilder[T]
	equal    func(T, T) bool
	checkedUnsignedChildren
}

func newCheckedDictionaryChildren[T Integer | Float | String](delegate DictionaryChildBuilder[T], equal func(T, T) bool, maxBytes uint64) checkedDictionaryChildren[T] {
	return checkedDictionaryChildren[T]{delegate: delegate, equal: equal, checkedUnsignedChildren: checkedUnsignedChildren{delegate: delegate, maxBytes: maxBytes}}
}

func (c checkedDictionaryChildren[T]) BuildValues(source array.ArrayCore[T]) (EncodedArray[T], error) {
	return checkedChild("dictionary values", c.delegate.BuildValues, c.equal, c.maxBytes)(source)
}

// SparseChildBuilder compresses sparse fills, values, and index children.
type SparseChildBuilder[T Integer | Float | String] interface {
	BuildFill(array.ArrayCore[T]) (EncodedArray[T], error)
	BuildValues(array.ArrayCore[T]) (EncodedArray[T], error)
	BuildUint8(array.ArrayCore[uint8]) (EncodedArray[uint8], error)
	BuildUint16(array.ArrayCore[uint16]) (EncodedArray[uint16], error)
	BuildUint32(array.ArrayCore[uint32]) (EncodedArray[uint32], error)
	BuildUint64(array.ArrayCore[uint64]) (EncodedArray[uint64], error)
}

type checkedSparseChildren[T Integer | Float | String] struct {
	delegate SparseChildBuilder[T]
	equal    func(T, T) bool
	checkedUnsignedChildren
}

func newCheckedSparseChildren[T Integer | Float | String](delegate SparseChildBuilder[T], equal func(T, T) bool, maxBytes uint64) checkedSparseChildren[T] {
	return checkedSparseChildren[T]{delegate: delegate, equal: equal, checkedUnsignedChildren: checkedUnsignedChildren{delegate: delegate, maxBytes: maxBytes}}
}

func (c checkedSparseChildren[T]) BuildFill(source array.ArrayCore[T]) (EncodedArray[T], error) {
	return checkedChild("sparse fill", c.delegate.BuildFill, c.equal, c.maxBytes)(source)
}

func (c checkedSparseChildren[T]) BuildValues(source array.ArrayCore[T]) (EncodedArray[T], error) {
	return checkedChild("sparse values", c.delegate.BuildValues, c.equal, c.maxBytes)(source)
}

// ALPChildBuilder compresses ALP's encoded integer and patch-index children.
type ALPChildBuilder interface {
	BuildInt32(array.ArrayCore[int32]) (EncodedArray[int32], error)
	BuildInt64(array.ArrayCore[int64]) (EncodedArray[int64], error)
	BuildUint8(array.ArrayCore[uint8]) (EncodedArray[uint8], error)
	BuildUint16(array.ArrayCore[uint16]) (EncodedArray[uint16], error)
	BuildUint32(array.ArrayCore[uint32]) (EncodedArray[uint32], error)
	BuildUint64(array.ArrayCore[uint64]) (EncodedArray[uint64], error)
}

// ALPRDChildBuilder compresses ALP-RD's right-part child.
type ALPRDChildBuilder interface {
	BuildUint8(array.ArrayCore[uint8]) (EncodedArray[uint8], error)
	BuildUint16(array.ArrayCore[uint16]) (EncodedArray[uint16], error)
	BuildUint32(array.ArrayCore[uint32]) (EncodedArray[uint32], error)
	BuildUint64(array.ArrayCore[uint64]) (EncodedArray[uint64], error)
}

type checkedALPChildren struct {
	delegate ALPChildBuilder
	checkedUnsignedChildren
}

func newCheckedALPChildren(delegate ALPChildBuilder, maxBytes uint64) checkedALPChildren {
	return checkedALPChildren{delegate: delegate, checkedUnsignedChildren: checkedUnsignedChildren{delegate: delegate, maxBytes: maxBytes}}
}

func (c checkedALPChildren) BuildInt32(source array.ArrayCore[int32]) (EncodedArray[int32], error) {
	return checkedChild("ALP int32", c.delegate.BuildInt32, array.CmpIntegers[int32], c.maxBytes)(source)
}

func (c checkedALPChildren) BuildInt64(source array.ArrayCore[int64]) (EncodedArray[int64], error) {
	return checkedChild("ALP int64", c.delegate.BuildInt64, array.CmpIntegers[int64], c.maxBytes)(source)
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
	maxBytes, err := validateBuildSource(arr, opts)
	if err != nil {
		return nil, err
	}
	if buildChild == nil {
		return nil, ErrBuilderRequired
	}
	return encodeDelta(arr, childBuilder[T](checkedChild("delta", buildChild, array.CmpIntegers[T], maxBytes)))
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
	return encodeIntegerDict(arr, nil, newCheckedDictionaryChildren(children, array.CmpIntegers[T], maxBytes), buildBudget{maxBytes: maxBytes})
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
	return encodeFloat32Dict(arr, nil, expectedDistinct, newCheckedDictionaryChildren(children, array.CmpFloatBits[float32], maxBytes), buildBudget{maxBytes: maxBytes})
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
	return encodeFloat64Dict(arr, nil, expectedDistinct, newCheckedDictionaryChildren(children, array.CmpFloatBits[float64], maxBytes), buildBudget{maxBytes: maxBytes})
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
	return encodeStringDict(arr, newCheckedDictionaryChildren(children, array.CmpStrings[string], maxBytes), buildBudget{maxBytes: maxBytes})
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
	return encodePrimitiveRunEndAs(arr, cmpPrimitiveBits[V], childBuilder[V](checkedChild("run values", buildRuns, cmpPrimitiveBits[V], maxBytes)), childBuilder[I](checkedChild("run ends", buildEnds, array.CmpIntegers[I], maxBytes)), buildBudget{maxBytes: maxBytes})
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
	return encodeStringRunEndAs(arr, array.CmpStrings[string], childBuilder[string](checkedChild("run values", buildRuns, array.CmpStrings[string], maxBytes)), childBuilder[I](checkedChild("run ends", buildEnds, array.CmpIntegers[I], maxBytes)), buildBudget{maxBytes: maxBytes})
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
	return encodeIntegerSparse(arr, newCheckedSparseChildren(children, array.CmpIntegers[T], maxBytes), buildBudget{maxBytes: maxBytes})
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
	return encodeIntegerSparseWithFill(arr, nullCount, newCheckedSparseChildren(children, array.CmpIntegers[T], maxBytes), buildBudget{maxBytes: maxBytes})
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
	return encodeFloatSparse(arr, newCheckedSparseChildren(children, array.CmpFloatBits[T], maxBytes), buildBudget{maxBytes: maxBytes})
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
	return encodeFloatSparseWithFill(arr, nullCount, newCheckedSparseChildren(children, array.CmpFloatBits[T], maxBytes), buildBudget{maxBytes: maxBytes})
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
	return encodeStringSparse(arr, newCheckedSparseChildren(children, array.CmpStrings[string], maxBytes), buildBudget{maxBytes: maxBytes})
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
	return encodeStringSparseWithFill(arr, nullCount, newCheckedSparseChildren(children, array.CmpStrings[string], maxBytes), buildBudget{maxBytes: maxBytes})
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
	maxBytes, err := validateBuildSource(arr, opts)
	if err != nil {
		return nil, err
	}
	if buildChild == nil {
		return nil, ErrBuilderRequired
	}
	return encodeZigZagAs(arr, childBuilder[U](checkedChild("zigzag", buildChild, array.CmpIntegers[U], maxBytes)))
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
	return encodeALP32(arr, newCheckedALPChildren(children, maxBytes), buildBudget{maxBytes: maxBytes})
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
	return encodeALP64(arr, newCheckedALPChildren(children, maxBytes), buildBudget{maxBytes: maxBytes})
}

// EncodeALPRD32 builds an ALP-RD float32 node and its children.
func EncodeALPRD32(arr array.ArrayCore[float32], children ALPRDChildBuilder, opts ...BuildOptions) (EncodedArray[float32], error) {
	maxBytes, err := validateBuildSource(arr, opts)
	if err != nil {
		return nil, err
	}
	if children == nil {
		return nil, ErrBuilderRequired
	}
	return encodeALPRD32(arr, checkedUnsignedChildren{delegate: children, maxBytes: maxBytes}, buildBudget{maxBytes: maxBytes})
}

// EncodeALPRD64 builds an ALP-RD float64 node and its children.
func EncodeALPRD64(arr array.ArrayCore[float64], children ALPRDChildBuilder, opts ...BuildOptions) (EncodedArray[float64], error) {
	maxBytes, err := validateBuildSource(arr, opts)
	if err != nil {
		return nil, err
	}
	if children == nil {
		return nil, ErrBuilderRequired
	}
	return encodeALPRD64(arr, checkedUnsignedChildren{delegate: children, maxBytes: maxBytes}, buildBudget{maxBytes: maxBytes})
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
	return encodeFSST(arr, checkedUnsignedChildren{delegate: offsetsChildren, maxBytes: maxBytes}, checkedUnsignedChildren{delegate: lengthsChildren, maxBytes: maxBytes}, buildBudget{maxBytes: maxBytes})
}

// NewNullable composes value and validity encodings into a nullable node.
func NewNullable[T Integer | Float | String](values EncodedArray[T], validity EncodedArray[uint8], nullCount uint64) (EncodedArray[T], error) {
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
	wantBytes := values.Length() / 8
	if values.Length()&7 != 0 {
		wantBytes++
	}
	if validity.Length() != wantBytes {
		return nil, fmt.Errorf("codec: nullable validity length = %d, want %d", validity.Length(), wantBytes)
	}
	validCount := uint64(0)
	for i := range wantBytes {
		value := validity.ValueAt(i)
		if i+1 == wantBytes && values.Length()&7 != 0 {
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
