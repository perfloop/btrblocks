package btrblocks

import "github.com/axiomhq/btrblocks/array"

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
		TypeIntCodecRaw:   func(arr array.Array[T], _ int) (Codec[T], error) { return NewRawCodec(arr), nil },
		TypeIntCodecDict:  func(arr array.Array[T], depth int) (Codec[T], error) { return NewDictIntegerCodec(arr, depth) },
		TypeIntCodecConst: func(arr array.Array[T], _ int) (Codec[T], error) { return NewConstIntegerCodec(arr) },
	}
}

func CompressInteger[T Integer](arr array.Array[T], depth int) Codec[T] {
	registry := intCodecRegistry[T]()
	ordered := []TypeIntCodec{TypeIntCodecConst, TypeIntCodecDict, TypeIntCodecRaw}
	var (
		selectedCodec Codec[T]
		selectedSize  uint64
	)
	for _, typ := range ordered {
		builder := registry[typ]
		if codec, err := builder(arr, depth); err == nil {
			if selectedCodec == nil || codec.BinarySize() < selectedSize {
				selectedCodec = codec
				selectedSize = codec.BinarySize()
			}
		}
	}
	return selectedCodec
}
