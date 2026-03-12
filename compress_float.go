package btrblocks

import "github.com/axiomhq/btrblocks/array"

var (
	_ Codec[float32] = (*RawCodec[float32])(nil)
	_ Codec[float64] = (*RawCodec[float64])(nil)
)

type TypeFloatCodec uint8

const (
	TypeFloatCodecRaw TypeFloatCodec = iota
	TypeFloatCodecDict
	TypeFloatCodecConst
)

func floatCodecRegistry[T Float]() map[TypeFloatCodec]codecBuilder[T] {
	return map[TypeFloatCodec]codecBuilder[T]{
		TypeFloatCodecRaw:   func(arr array.Array[T], _ int) (Codec[T], error) { return NewRawCodec(arr), nil },
		TypeFloatCodecDict:  func(arr array.Array[T], depth int) (Codec[T], error) { return NewDictFloatCodec(arr, depth) },
		TypeFloatCodecConst: func(arr array.Array[T], _ int) (Codec[T], error) { return NewConstFloatCodec(arr) },
	}
}

func CompressFloat[T Float](arr array.Array[T], depth int) Codec[T] {
	registry := floatCodecRegistry[T]()
	ordered := []TypeFloatCodec{TypeFloatCodecConst, TypeFloatCodecDict, TypeFloatCodecRaw}
	var (
		bestCodec Codec[T]
		bestSize  uint64
	)
	for _, typ := range ordered {
		builder := registry[typ]
		if codec, err := builder(arr, depth); err == nil {
			// if codec is better than best codec, update best codec and best size
			if bestCodec == nil || codec.BinarySize() < bestSize {
				bestCodec = codec
				bestSize = codec.BinarySize()
			}
		}
	}
	return bestCodec
}
