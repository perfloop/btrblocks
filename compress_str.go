package btrblocks

import "github.com/axiomhq/btrblocks/array"

var (
	_ Codec[string] = (*RawCodec[string])(nil)
)

type TypeStringCodec uint8

const (
	TypeStringCodecRaw TypeStringCodec = iota
	TypeStringCodecDict
	TypeStringCodecConst
)

func stringCodecRegistry() map[TypeStringCodec]codecBuilder[string] {
	return map[TypeStringCodec]codecBuilder[string]{
		TypeStringCodecRaw:   func(arr array.Array[string], _ int) (Codec[string], error) { return NewRawCodec(arr), nil },
		TypeStringCodecDict:  func(arr array.Array[string], depth int) (Codec[string], error) { return NewDictStringCodec(arr, depth) },
		TypeStringCodecConst: func(arr array.Array[string], _ int) (Codec[string], error) { return NewConstStringCodec(arr) },
	}
}

func CompressString(arr array.Array[string], depth int) Codec[string] {
	registry := stringCodecRegistry()
	ordered := []TypeStringCodec{TypeStringCodecConst, TypeStringCodecDict, TypeStringCodecRaw}
	var (
		bestCodec Codec[string]
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
