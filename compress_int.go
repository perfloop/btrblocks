package btrblocks

import "github.com/axiomhq/btrblocks/array"

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

func integerBuilders[T Integer]() []taggedBuilder[T] {
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
		{
			kind: CodecTypeSparse,
			build: func(arr array.Array[T], depth int, excl codecExcludes) (Codec[T], error) {
				return NewSparseIntegerCodec(arr, depth, excl)
			},
		},
	}
}

func signedIntegerBuilders[T SignedInteger]() []taggedBuilder[T] {
	return []taggedBuilder[T]{
		{func(arr array.Array[T], depth int, excl codecExcludes) (Codec[T], error) {
			return NewZigzagCodec(arr, depth, excl)
		}, CodecTypeZigzag},
	}
}

func unsignedIntegerBuilders[T UnsignedInteger]() []taggedBuilder[T] {
	return []taggedBuilder[T]{
		{func(arr array.Array[T], _ int, _ codecExcludes) (Codec[T], error) {
			return NewBitpackingCodec(arr), nil
		}, CodecTypeBitpacking},
		{func(arr array.Array[T], depth int, excl codecExcludes) (Codec[T], error) {
			return NewFoRCodec(arr, depth, excl)
		}, CodecTypeFoR},
	}
}

func CompressSignedInteger[T SignedInteger](arr array.Array[T], depth int, excludes codecExcludes) Codec[T] {
	builders := append(integerBuilders[T](), signedIntegerBuilders[T]()...)
	return selectBest(arr, depth, builders, excludes)
}

func CompressUnsignedInteger[T UnsignedInteger](arr array.Array[T], depth int, excludes codecExcludes) Codec[T] {
	builders := append(integerBuilders[T](), unsignedIntegerBuilders[T]()...)
	return selectBest(arr, depth, builders, excludes)
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
