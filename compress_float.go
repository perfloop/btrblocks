package btrblocks

import "github.com/axiomhq/btrblocks/array"

var (
	_ Codec[float32] = (*RawCodec[float32])(nil)
	_ Codec[float64] = (*RawCodec[float64])(nil)
)

func floatBuilders[T Float]() []taggedBuilder[T] {
	return []taggedBuilder[T]{
		{
			kind:  CodecTypeRaw,
			build: func(arr array.Array[T], _ int, _ codecExcludes) (Codec[T], error) { return NewRawCodec(arr), nil },
		},
		{
			kind:  CodecTypeConst,
			build: func(arr array.Array[T], _ int, _ codecExcludes) (Codec[T], error) { return NewConstFloatCodec(arr) },
		},
		{
			kind: CodecTypeDict,
			build: func(arr array.Array[T], depth int, excl codecExcludes) (Codec[T], error) {
				return NewDictFloatCodec(arr, depth, excl)
			},
		},
		{
			kind: CodecTypeRunend,
			build: func(arr array.Array[T], depth int, excl codecExcludes) (Codec[T], error) {
				return NewRunendFloatCodec(arr, depth, excl)
			},
		},
		{
			kind: CodecTypeSparse,
			build: func(arr array.Array[T], depth int, excl codecExcludes) (Codec[T], error) {
				return NewSparseFloatCodec(arr, depth, excl)
			},
		},
	}
}

func CompressFloat[T Float](arr array.Array[T], depth int, excludes codecExcludes) Codec[T] {
	return selectBest(arr, depth, floatBuilders[T](), excludes)
}
