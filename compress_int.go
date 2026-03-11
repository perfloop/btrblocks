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

func intCodecRegistry[T Integer]() map[TypeIntCodec]codecBuilder[T] {
	return map[TypeIntCodec]codecBuilder[T]{
		TypeIntCodecRaw:   func(data []T, _ int) (Codec[T], error) { return NewRawCodec(data), nil },
		TypeIntCodecDict:  func(data []T, depth int) (Codec[T], error) { return NewDictIntegerCodec(data, depth) },
		TypeIntCodecConst: func(data []T, _ int) (Codec[T], error) { return NewConstIntegerCodec(data) },
	}
}

func CompressInteger[T Integer](data []T, depth int) Codec[T] {
	registry := intCodecRegistry[T]()
	var (
		bestCodec Codec[T]
		bestSize  uint64
	)
	for _, builder := range registry {
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
