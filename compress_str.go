package btrblocks

import "github.com/axiomhq/btrblocks/array"

var (
	_ Codec[string] = (*RawCodec[string])(nil)
)

func stringBuilders() []codecBuilder[string] {
	return []codecBuilder[string]{
		func(arr array.Array[string], _ int) (Codec[string], error) { return NewRawCodec(arr), nil },
		func(arr array.Array[string], _ int) (Codec[string], error) { return NewConstStringCodec(arr) },
		func(arr array.Array[string], depth int) (Codec[string], error) { return NewDictStringCodec(arr, depth) },
		func(arr array.Array[string], depth int) (Codec[string], error) {
			return newRunendCodecFromArray(arr, cmpStrings[string], depth)
		},
	}
}

func CompressString(arr array.Array[string], depth int) Codec[string] {
	return selectBest(arr, depth, stringBuilders())
}
