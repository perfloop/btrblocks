package btrblocks

import "github.com/axiomhq/btrblocks/array"

var (
	_ Codec[float32] = (*RawCodec[float32])(nil)
	_ Codec[float64] = (*RawCodec[float64])(nil)
)

func floatBuilders[T Float]() []taggedBuilder[T] {
	return []taggedBuilder[T]{
		{func(arr array.Array[T], _ int) (Codec[T], error) { return NewRawCodec(arr), nil }, CodecTypeRaw},
		{func(arr array.Array[T], _ int) (Codec[T], error) { return NewConstFloatCodec(arr) }, CodecTypeConst},
		{func(arr array.Array[T], depth int) (Codec[T], error) { return NewDictFloatCodec(arr, depth) }, CodecTypeDict},
		{func(arr array.Array[T], depth int) (Codec[T], error) {
			return NewRunendFloatCodec(arr, depth)
		}, CodecTypeRunend},
	}
}

func CompressFloat[T Float](arr array.Array[T], depth int) Codec[T] {
	return selectBest(arr, depth, floatBuilders[T]())
}
