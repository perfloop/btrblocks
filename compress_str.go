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

func stringCodecRegistry[T String]() map[TypeStringCodec]codecBuilder[T] {
	return map[TypeStringCodec]codecBuilder[T]{
		TypeStringCodecRaw:   func(data []T) (Codec[T], error) { return NewRawCodec(data), nil },
		TypeStringCodecDict:  func(data []T) (Codec[T], error) { return NewDictStringCodec(data) },
		TypeStringCodecConst: func(data []T) (Codec[T], error) { return NewConstStringCodec(data) },
	}
}

func CompressString[T String](data []T) Codec[T] {
	registry := stringCodecRegistry[T]()
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
