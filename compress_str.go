package btrblocks

import "github.com/axiomhq/btrblocks/array"

var (
	_ Codec[string] = (*RawCodec[string])(nil)
)

func stringBuilders() []taggedBuilder[string] {
	return []taggedBuilder[string]{
		{func(arr array.Array[string], _ int) (Codec[string], error) { return NewRawCodec(arr), nil }, CodecTypeRaw},
		{func(arr array.Array[string], _ int) (Codec[string], error) { return NewConstStringCodec(arr) }, CodecTypeConst},
		{func(arr array.Array[string], d int) (Codec[string], error) { return NewDictStringCodec(arr, d) }, CodecTypeDict},
		{func(arr array.Array[string], d int) (Codec[string], error) { return NewRunendStringCodec(arr, d) }, CodecTypeRunend},
	}
}

func CompressString(arr array.Array[string], depth int) Codec[string] {
	return selectBest(arr, depth, stringBuilders())
}
