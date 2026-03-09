package btrblocks

var (
	_ Codec[string] = (*RawCodec[string])(nil)
)

type TypeStringCodec uint8

const (
	TypeStringCodecRaw TypeStringCodec = iota
	TypeStringCodecDict
	TypeStringCodecConst
)

type stringCodecBuilder[T String] func([]T) (Codec[T], error)

func stringCodecRegistry[T String]() map[TypeStringCodec]stringCodecBuilder[T] {
	return map[TypeStringCodec]stringCodecBuilder[T]{
		TypeStringCodecRaw:   func(data []T) (Codec[T], error) { return NewRawCodec(data), nil },
		TypeStringCodecDict:  func(data []T) (Codec[T], error) { return NewDictCodec(data), nil },
		TypeStringCodecConst: func(data []T) (Codec[T], error) { return NewConstCodec(data) },
	}
}

func CompressString[T String](data []T, exclude []TypeStringCodec) Codec[T] {
	registry := stringCodecRegistry[T]()
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
