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

type codecBuilder[T Integer | Float | String] func(array.Array[T], []T, int) (Codec[T], error)

func selectBest[T Integer | Float | String](arr array.Array[T], data []T, depth int, builders []codecBuilder[T]) Codec[T] {
	var (
		best     Codec[T]
		bestSize uint64
	)
	for _, build := range builders {
		if c, err := build(arr, data, depth); err == nil {
			s := c.BinarySize()
			if best == nil || s < bestSize {
				best = c
				bestSize = s
			}
		}
	}
	return best
}

func integerBuilders[T Integer]() []codecBuilder[T] {
	return []codecBuilder[T]{
		func(arr array.Array[T], _ []T, _ int) (Codec[T], error) { return NewRawCodec(arr), nil },
		func(arr array.Array[T], _ []T, _ int) (Codec[T], error) { return NewConstIntegerCodec(arr) },
		func(arr array.Array[T], _ []T, depth int) (Codec[T], error) { return NewDictIntegerCodec(arr, depth) },
		func(_ array.Array[T], data []T, depth int) (Codec[T], error) { return NewRunendIntegerCodec(data, depth) },
	}
}

func signedIntegerBuilders[T SignedInteger]() []codecBuilder[T] {
	return []codecBuilder[T]{
		func(_ array.Array[T], data []T, depth int) (Codec[T], error) { return NewZigzagCodec(data, depth) },
	}
}

func unsignedIntegerBuilders[T UnsignedInteger]() []codecBuilder[T] {
	return []codecBuilder[T]{
		func(_ array.Array[T], data []T, _ int) (Codec[T], error) { return NewBitpackingCodec(data), nil },
	}
}

func CompressSignedInteger[T SignedInteger](arr array.Array[T], depth int) Codec[T] {
	data := make([]T, arr.Length())
	arr.CopyTo(data)
	builders := append(integerBuilders[T](), signedIntegerBuilders[T]()...)
	return selectBest(arr, data, depth, builders)
}

func CompressUnsignedInteger[T UnsignedInteger](arr array.Array[T], depth int) Codec[T] {
	data := make([]T, arr.Length())
	arr.CopyTo(data)
	builders := append(integerBuilders[T](), unsignedIntegerBuilders[T]()...)
	return selectBest(arr, data, depth, builders)
}

func CompressInteger[T Integer](arr array.Array[T], depth int) Codec[T] {
	var zero T
	switch any(zero).(type) {
	case int8:
		return any(CompressSignedInteger(any(arr).(array.Array[int8]), depth)).(Codec[T])
	case int16:
		return any(CompressSignedInteger(any(arr).(array.Array[int16]), depth)).(Codec[T])
	case int32:
		return any(CompressSignedInteger(any(arr).(array.Array[int32]), depth)).(Codec[T])
	case int64:
		return any(CompressSignedInteger(any(arr).(array.Array[int64]), depth)).(Codec[T])
	case uint8:
		return any(CompressUnsignedInteger(any(arr).(array.Array[uint8]), depth)).(Codec[T])
	case uint16:
		return any(CompressUnsignedInteger(any(arr).(array.Array[uint16]), depth)).(Codec[T])
	case uint32:
		return any(CompressUnsignedInteger(any(arr).(array.Array[uint32]), depth)).(Codec[T])
	case uint64:
		return any(CompressUnsignedInteger(any(arr).(array.Array[uint64]), depth)).(Codec[T])
	default:
		return nil
	}
}
