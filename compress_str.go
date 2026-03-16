package btrblocks

import "github.com/axiomhq/btrblocks/array"

var (
	_ Codec[string] = (*RawCodec[string])(nil)
)

func stringBuilders() []taggedBuilder[string] {
	return []taggedBuilder[string]{
		{
			kind: CodecTypeRaw,
			build: func(arr array.Array[string], _ int, _ codecExcludes) (Codec[string], error) {
				return NewRawCodec(arr), nil
			},
		}, {
			kind: CodecTypeConst,
			build: func(arr array.Array[string], _ int, _ codecExcludes) (Codec[string], error) {
				return NewConstStringCodec(arr)
			},
		}, {
			kind: CodecTypeDict,
			build: func(arr array.Array[string], d int, excl codecExcludes) (Codec[string], error) {
				return NewDictStringCodec(arr, d, excl)
			},
		}, {
			kind: CodecTypeRunend,
			build: func(arr array.Array[string], d int, excl codecExcludes) (Codec[string], error) {
				return NewRunendStringCodec(arr, d, excl)
			},
		}, {
			kind: CodecTypeSparse,
			build: func(arr array.Array[string], d int, excl codecExcludes) (Codec[string], error) {
				return NewSparseStringCodec(arr, d, excl)
			},
		},
	}
}

func CompressString(arr array.Array[string], depth int, excludes codecExcludes) Codec[string] {
	return selectBest(arr, depth, stringBuilders(), excludes)
}
