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

type floatCodecBuilder[T Float] func([]T) (Codec[T], error)

func floatCodecRegistry[T Float]() map[TypeFloatCodec]floatCodecBuilder[T] {
	return map[TypeFloatCodec]floatCodecBuilder[T]{
		TypeFloatCodecRaw:   func(data []T) (Codec[T], error) { return NewRawCodec(data), nil },
		TypeFloatCodecDict:  func(data []T) (Codec[T], error) { return NewDictCodec(data), nil },
		TypeFloatCodecConst: func(data []T) (Codec[T], error) { return NewConstCodec(data) },
	}
}

func CompressFloat[T Float](data []T, exclude []TypeFloatCodec) Codec[T] {
	registry := floatCodecRegistry[T]()
	for _, codec := range exclude {
		delete(registry, codec)
	}

	var (
		bestCodec Codec[T]
		bestSize  uint64
	)
	for _, builder := range registry {
		if codec, err := builder(data); err == nil {
			// if codec is better than best codec, update best codec and best size
			if bestCodec == nil || codec.BinarySize() < bestSize {
				bestCodec = codec
				bestSize = codec.BinarySize()
			}
		}
	}
	return bestCodec
}
