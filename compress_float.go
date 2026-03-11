package btrblocks

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
		TypeFloatCodecRaw:   func(data []T, _ int) (Codec[T], error) { return NewRawCodec(data), nil },
		TypeFloatCodecDict:  func(data []T, depth int) (Codec[T], error) { return NewDictFloatCodec(data, depth) },
		TypeFloatCodecConst: func(data []T, _ int) (Codec[T], error) { return NewConstFloatCodec(data) },
	}
}

func CompressFloat[T Float](data []T, depth int) Codec[T] {
	registry := floatCodecRegistry[T]()
	ordered := []TypeFloatCodec{TypeFloatCodecConst, TypeFloatCodecDict, TypeFloatCodecRaw}
	var (
		bestCodec Codec[T]
		bestSize  uint64
	)
	for _, typ := range ordered {
		builder := registry[typ]
		if codec, err := builder(data, depth); err == nil {
			// if codec is better than best codec, update best codec and best size
			if bestCodec == nil || codec.BinarySize() < bestSize {
				bestCodec = codec
				bestSize = codec.BinarySize()
			}
		}
	}
	return bestCodec
}
