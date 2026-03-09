package btrblocks

var (
	_ Codec[int8]   = (*RawCodec[int8])(nil)
	_ Codec[int16]  = (*RawCodec[int16])(nil)
	_ Codec[int32]  = (*RawCodec[int32])(nil)
	_ Codec[int64]  = (*RawCodec[int64])(nil)
	_ Codec[uint8]  = (*RawCodec[uint8])(nil)
	_ Codec[uint16] = (*RawCodec[uint16])(nil)
	_ Codec[uint32] = (*RawCodec[uint32])(nil)
	_ Codec[uint64] = (*RawCodec[uint64])(nil)
)

type TypeIntCodec uint8

const (
	TypeIntCodecRaw TypeIntCodec = iota
	TypeIntCodecDict
	TypeIntCodecConst
)

type intCodecBuilder[T Integer] func([]T) (Codec[T], error)

func intCodecRegistry[T Integer]() map[TypeIntCodec]intCodecBuilder[T] {
	return map[TypeIntCodec]intCodecBuilder[T]{
		TypeIntCodecRaw:   func(data []T) (Codec[T], error) { return NewRawCodec(data), nil },
		TypeIntCodecDict:  func(data []T) (Codec[T], error) { return NewDictCodec(data), nil },
		TypeIntCodecConst: func(data []T) (Codec[T], error) { return NewConstCodec(data) },
	}
}

func CompressInteger[T Integer](data []T, exclude []TypeIntCodec) Codec[T] {
	registry := intCodecRegistry[T]()
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
