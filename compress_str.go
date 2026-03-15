package btrblocks

import "github.com/axiomhq/btrblocks/array"

var (
	_ Codec[string] = (*RawCodec[string])(nil)
)

func stringBuilders() []codecBuilder[string] {
	return []codecBuilder[string]{
		func(arr array.Array[string], _ []string, _ int) (Codec[string], error) { return NewRawCodec(arr), nil },
		func(arr array.Array[string], _ []string, _ int) (Codec[string], error) { return NewConstStringCodec(arr) },
		func(arr array.Array[string], _ []string, depth int) (Codec[string], error) { return NewDictStringCodec(arr, depth) },
		func(_ array.Array[string], data []string, depth int) (Codec[string], error) { return NewRunendStringCodec(data, depth) },
	}
}

func CompressString(arr array.Array[string], depth int) Codec[string] {
	data := make([]string, arr.Length())
	arr.CopyTo(data)
	return selectBest(arr, data, depth, stringBuilders())
}
