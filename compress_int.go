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

type codecBuilder[T Integer | Float | String] func(array.Array[T], int) (Codec[T], error)

type taggedBuilder[T Integer | Float | String] struct {
	build codecBuilder[T]
	kind  CodecType
}

func selectBest[T Integer | Float | String](arr array.Array[T], depth int, builders []taggedBuilder[T]) Codec[T] {
	if arr.Length() < sampleThreshold {
		return selectBestAll(arr, depth, builders)
	}

	// Stratified sample: evaluate all builders on ~1% of data.
	sample := sampleArray(arr)
	hints := computeSampleHints(sample)

	var (
		bestIdx  = -1
		bestSize uint64
		constIdx = -1
	)
	for i, tb := range builders {
		// Const: skip on samples — false positive risk.
		if tb.kind == CodecTypeConst {
			constIdx = i
			continue
		}
		// Stats-based rejection (Vortex Level 1).
		if hints.shouldSkip(tb.kind) {
			continue
		}
		c, err := tb.build(sample, depth)
		if err != nil {
			continue
		}
		s := c.BinarySize()
		if bestIdx < 0 || s < bestSize {
			bestIdx = i
			bestSize = s
		}
	}

	// Run the sample winner on full data.
	var best Codec[T]
	if bestIdx >= 0 {
		if c, err := builders[bestIdx].build(arr, depth); err == nil {
			best = c
		}
	}

	// Always try Const on full data — it's an O(n) comparison scan with
	// no heavy allocation, and sampling can't reliably detect it.
	if constIdx >= 0 {
		if c, err := builders[constIdx].build(arr, depth); err == nil {
			if best == nil || c.BinarySize() < best.BinarySize() {
				best = c
			}
		}
	}

	if best != nil {
		return best
	}
	// Fallback: Raw never fails.
	return NewRawCodec(arr)
}

// selectBestAll evaluates every builder on arr and returns the smallest codec.
func selectBestAll[T Integer | Float | String](arr array.Array[T], depth int, builders []taggedBuilder[T]) Codec[T] {
	var (
		best     Codec[T]
		bestSize uint64
	)
	for _, tb := range builders {
		if c, err := tb.build(arr, depth); err == nil {
			s := c.BinarySize()
			if best == nil || s < bestSize {
				best = c
				bestSize = s
			}
		}
	}
	return best
}

func integerBuilders[T Integer]() []taggedBuilder[T] {
	return []taggedBuilder[T]{
		{func(arr array.Array[T], _ int) (Codec[T], error) { return NewRawCodec(arr), nil }, CodecTypeRaw},
		{func(arr array.Array[T], _ int) (Codec[T], error) { return NewConstIntegerCodec(arr) }, CodecTypeConst},
		{func(arr array.Array[T], depth int) (Codec[T], error) { return NewDictIntegerCodec(arr, depth) }, CodecTypeDict},
		{func(arr array.Array[T], depth int) (Codec[T], error) {
			return NewRunendIntegerCodec(arr, depth)
		}, CodecTypeRunend},
	}
}

func signedIntegerBuilders[T SignedInteger]() []taggedBuilder[T] {
	return []taggedBuilder[T]{
		{func(arr array.Array[T], depth int) (Codec[T], error) { return NewZigzagCodec(arr, depth) }, CodecTypeZigzag},
	}
}

func unsignedIntegerBuilders[T UnsignedInteger]() []taggedBuilder[T] {
	return []taggedBuilder[T]{
		{func(arr array.Array[T], _ int) (Codec[T], error) { return NewBitpackingCodec(arr), nil }, CodecTypeBitpacking},
	}
}

func CompressSignedInteger[T SignedInteger](arr array.Array[T], depth int) Codec[T] {
	builders := append(integerBuilders[T](), signedIntegerBuilders[T]()...)
	return selectBest(arr, depth, builders)
}

func CompressUnsignedInteger[T UnsignedInteger](arr array.Array[T], depth int) Codec[T] {
	builders := append(integerBuilders[T](), unsignedIntegerBuilders[T]()...)
	return selectBest(arr, depth, builders)
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
