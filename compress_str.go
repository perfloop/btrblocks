package btrblocks

import "github.com/axiomhq/btrblocks/array"

var (
	_ Codec[string] = (*RawCodec[string])(nil)
)

// stringBuilders returns builders in trial order:
// Raw→Dict→Const→Sparse→RunEnd (we don't have FSST/Zstd yet).
func stringBuilders() []taggedBuilder[string] {
	return []taggedBuilder[string]{
		{
			kind: CodecTypeRaw,
			build: func(arr array.Array[string], _ int, _ codecExcludes) (Codec[string], error) {
				return NewRawCodec(arr), nil
			},
		}, {
			kind: CodecTypeDict,
			build: func(arr array.Array[string], d int, excl codecExcludes) (Codec[string], error) {
				return NewDictStringCodec(arr, d, excl)
			},
		}, {
			kind: CodecTypeConst,
			build: func(arr array.Array[string], _ int, _ codecExcludes) (Codec[string], error) {
				return NewConstStringCodec(arr)
			},
		}, {
			kind: CodecTypeSparse,
			build: func(arr array.Array[string], d int, excl codecExcludes) (Codec[string], error) {
				return NewSparseStringCodec(arr, d, excl)
			},
		}, {
			kind: CodecTypeRunend,
			build: func(arr array.Array[string], d int, excl codecExcludes) (Codec[string], error) {
				return NewRunendStringCodec(arr, d, excl)
			},
		},
	}
}

func CompressString(arr array.Array[string], depth int, excludes codecExcludes) Codec[string] {
	return selectBest(arr, depth, stringBuilders(), excludes)
}
