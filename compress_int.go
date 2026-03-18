package btrblocks

import (
	"math/bits"

	"github.com/axiomhq/btrblocks/array"
)

var (
	_ Codec[int8]   = (*RawCodec[int8])(nil)
	_ Codec[int16]  = (*RawCodec[int16])(nil)
	_ Codec[int32]  = (*RawCodec[int32])(nil)
	_ Codec[int64]  = (*RawCodec[int64])(nil)
	_ Codec[uint8]  = (*RawCodec[uint8])(nil)
	_ Codec[uint16] = (*RawCodec[uint16])(nil)
	_ Codec[uint32] = (*RawCodec[uint32])(nil)
	_ Codec[uint64] = (*RawCodec[uint64])(nil)
)

// integerBaseBuilders returns the shared head builders (Raw, Const) that
// precede type-specific codecs in trial order.
func integerBaseBuilders[T Integer]() []taggedBuilder[T] {
	return []taggedBuilder[T]{
		{
			kind:  CodecTypeRaw,
			build: func(arr array.Array[T], _ int, _ codecExcludes) (Codec[T], error) { return NewRawCodec(arr), nil },
		},
		{
			kind:  CodecTypeConst,
			build: func(arr array.Array[T], _ int, _ codecExcludes) (Codec[T], error) { return NewConstIntegerCodec(arr) },
		},
		{
			kind:  CodecTypeSequence,
			build: func(arr array.Array[T], depth int, excl codecExcludes) (Codec[T], error) { return NewSequenceCodec(arr, depth, excl) },
		},
	}
}

// integerTailBuilders returns the shared tail builders that follow
// type-specific codecs in trial order: Delta→Sparse→Dict→RunEnd.
func integerTailBuilders[T Integer]() []taggedBuilder[T] {
	return []taggedBuilder[T]{
		{
			kind: CodecTypeSparse,
			build: func(arr array.Array[T], depth int, excl codecExcludes) (Codec[T], error) {
				return NewSparseIntegerCodec(arr, depth, excl)
			},
		},
		{
			kind: CodecTypeDict,
			build: func(arr array.Array[T], depth int, excl codecExcludes) (Codec[T], error) {
				return NewDictIntegerCodec(arr, depth, excl)
			},
		},
		{
			kind: CodecTypeRunend,
			build: func(arr array.Array[T], depth int, excl codecExcludes) (Codec[T], error) {
				return NewRunendIntegerCodec(arr, depth, excl)
			},
		},
	}
}

// CompressSignedInteger builds codecs in trial order:
// Constant→ZigZag→Sparse→Dict→RunEnd (Raw as baseline).
func CompressSignedInteger[T SignedInteger](arr array.Array[T], depth int, excludes codecExcludes) Codec[T] {
	stats := computeSignedIntStats(arr)
	if !stats.hasNegative {
		excludes = excludes.with(CodecTypeZigzag)
	}
	builders := integerBaseBuilders[T]()
	builders = append(builders, taggedBuilder[T]{
		kind: CodecTypeZigzag,
		build: func(arr array.Array[T], depth int, excl codecExcludes) (Codec[T], error) {
			return NewZigzagCodec(arr, depth, excl)
		},
	})
	builders = append(builders, integerTailBuilders[T]()...)
	return selectBest(arr, depth, builders, excludes, stats.baseStats)
}

// CompressUnsignedInteger builds codecs in trial order:
// Constant→FoR→BitPacking→Sparse→Dict→RunEnd (Raw as baseline).
func CompressUnsignedInteger[T UnsignedInteger](arr array.Array[T], depth int, excludes codecExcludes) Codec[T] {
	stats := computeUnsignedIntStats(arr)
	if bits.Len64(uint64(stats.max-stats.min)) >= bits.Len64(uint64(stats.max)) {
		excludes = excludes.with(CodecTypeFoR)
	}
	builders := integerBaseBuilders[T]()
	builders = append(builders, taggedBuilder[T]{
		kind: CodecTypeFoR,
		build: func(arr array.Array[T], depth int, excl codecExcludes) (Codec[T], error) {
			return NewFoRCodec(arr, depth, excl)
		},
	}, taggedBuilder[T]{
		kind: CodecTypeBitpacking,
		build: func(arr array.Array[T], _ int, _ codecExcludes) (Codec[T], error) {
			return NewBitpackingCodec(arr), nil
		},
	})
	builders = append(builders, integerTailBuilders[T]()...)
	return selectBest(arr, depth, builders, excludes, stats.baseStats)
}

func CompressInteger[T Integer](arr array.Array[T], depth int, excludes codecExcludes) Codec[T] {
	var zero T
	switch any(zero).(type) {
	case int8:
		return any(CompressSignedInteger(any(arr).(array.Array[int8]), depth, excludes)).(Codec[T])
	case int16:
		return any(CompressSignedInteger(any(arr).(array.Array[int16]), depth, excludes)).(Codec[T])
	case int32:
		return any(CompressSignedInteger(any(arr).(array.Array[int32]), depth, excludes)).(Codec[T])
	case int64:
		return any(CompressSignedInteger(any(arr).(array.Array[int64]), depth, excludes)).(Codec[T])
	case uint8:
		return any(CompressUnsignedInteger(any(arr).(array.Array[uint8]), depth, excludes)).(Codec[T])
	case uint16:
		return any(CompressUnsignedInteger(any(arr).(array.Array[uint16]), depth, excludes)).(Codec[T])
	case uint32:
		return any(CompressUnsignedInteger(any(arr).(array.Array[uint32]), depth, excludes)).(Codec[T])
	case uint64:
		return any(CompressUnsignedInteger(any(arr).(array.Array[uint64]), depth, excludes)).(Codec[T])
	default:
		return nil
	}
}
