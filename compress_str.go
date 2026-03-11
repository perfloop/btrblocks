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
		TypeStringCodecRaw:   func(data []T, _ int) (Codec[T], error) { return NewRawCodec(data), nil },
		TypeStringCodecDict:  func(data []T, depth int) (Codec[T], error) { return NewDictStringCodec(data, depth) },
		TypeStringCodecConst: func(data []T, _ int) (Codec[T], error) { return NewConstStringCodec(data) },
	}
}

func CompressString[T String](data []T, depth int) Codec[T] {
	registry := stringCodecRegistry[T]()
	ordered := []TypeStringCodec{TypeStringCodecConst, TypeStringCodecDict, TypeStringCodecRaw}
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
