package btrblocks

type ZigzagCodec[T Integer] struct {
	data Codec[T]
}

func NewZigzagCodec[T Integer](vals []T, depth int) (*ZigzagCodec[T], error) {
	if depth <= 0 {
		return nil, errDepthExhausted
	}

	return &ZigzagCodec[T]{data: CompressInteger(vals, depth-1)}, nil
}
