package btrblocks

import "github.com/axiomhq/btrblocks/array"

var (
	_ Codec[float32] = (*RawCodec[float32])(nil)
	_ Codec[float64] = (*RawCodec[float64])(nil)
)

func floatBuilders[T Float]() []codecBuilder[T] {
	return []codecBuilder[T]{
		func(arr array.Array[T], _ int) (Codec[T], error) { return NewRawCodec(arr), nil },
		func(arr array.Array[T], _ int) (Codec[T], error) { return NewConstFloatCodec(arr) },
		func(arr array.Array[T], depth int) (Codec[T], error) { return NewDictFloatCodec(arr, depth) },
		func(arr array.Array[T], depth int) (Codec[T], error) {
			return newRunendCodecFromArray(arr, cmpFloats[T], depth)
		},
	}
}

func CompressFloat[T Float](arr array.Array[T], depth int) Codec[T] {
	return selectBest(arr, depth, floatBuilders[T]())
}
