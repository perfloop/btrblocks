package compress

import (
	"fmt"

	"github.com/axiomhq/btrblocks/array"
	"github.com/axiomhq/btrblocks/codec"
)

// SignedArray encodes a signed integer array according to opts. Raw fallback
// may continue to reference arr after SignedArray returns.
func SignedArray[T array.SignedInteger](arr array.Array[T], opts Options) (EncodedArray[T], error) {
	if arr == nil {
		return nil, fmt.Errorf("compress: nil signed array")
	}
	return compressSigned(arr, newPlanContext(opts))
}

// UnsignedArray encodes an unsigned integer array according to opts. Raw
// fallback may continue to reference arr after UnsignedArray returns.
func UnsignedArray[T array.UnsignedInteger](arr array.Array[T], opts Options) (EncodedArray[T], error) {
	if arr == nil {
		return nil, fmt.Errorf("compress: nil unsigned array")
	}
	return compressUnsigned(arr, newPlanContext(opts))
}

// Float32Array encodes a float32 array according to opts. Raw fallback may
// continue to reference arr after Float32Array returns.
func Float32Array(arr array.Array[float32], opts Options) (EncodedArray[float32], error) {
	if arr == nil {
		return nil, fmt.Errorf("compress: nil float32 array")
	}
	return compressFloat32(arr, newPlanContext(opts))
}

// Float64Array encodes a float64 array according to opts. Raw fallback may
// continue to reference arr after Float64Array returns.
func Float64Array(arr array.Array[float64], opts Options) (EncodedArray[float64], error) {
	if arr == nil {
		return nil, fmt.Errorf("compress: nil float64 array")
	}
	return compressFloat64(arr, newPlanContext(opts))
}

// StringArray encodes a string array according to opts. Raw fallback may
// continue to reference arr after StringArray returns.
func StringArray(arr array.Array[string], opts Options) (EncodedArray[string], error) {
	if arr == nil {
		return nil, fmt.Errorf("compress: nil string array")
	}
	return compressString(arr, newPlanContext(opts))
}

// compressFamilyNullable holds the per-family compression entry once: the
// null-free fast path straight into the planner, and otherwise the masked
// nullable wiring with comp's own exclusion policy deciding the sparse route.
// mask is invoked lazily so the fast path never builds a masked view.
func compressFamilyNullable[T array.Integer | array.Float | array.String, S statsSource[T]](
	arr array.Array[T],
	ctx planContext,
	comp compressor[T, S],
	mask func(array.Array[T]) *maskedArray[T],
	nullSparse func(array.ArrayCore[T], planContext) (EncodedArray[T], error),
) (EncodedArray[T], error) {
	nullCount := arr.NullCount()
	if nullCount == 0 {
		return compressWith(arr, ctx, comp)
	}
	masked := mask(arr)
	var values EncodedArray[T]
	var validity EncodedArray[uint8]
	if nullCount == arr.Length() {
		var zero T
		body, err := masked.materialize([]T{zero})
		if err != nil {
			return nil, fmt.Errorf("codec: build all-null constant: %w", err)
		}
		values, err = codec.NewConstArray(arr.Length(), body)
		if err != nil {
			return nil, fmt.Errorf("codec: build all-null values: %w", err)
		}
		validityLength := arr.Length() / 8
		if arr.Length()&7 != 0 {
			validityLength++
		}
		validity, err = codec.NewConstArray(validityLength, array.NewPrimitivesUnsafe([]uint8{0}))
		if err != nil {
			return nil, fmt.Errorf("codec: build all-null validity: %w", err)
		}
	} else {
		valueCtx := ctx.withNullCount(nullCount)
		var err error
		if useNullSparse(arr.Length(), nullCount, ctx, comp.IsExcluded(ctx, CodecTypeSparse)) {
			values, err = nullSparse(masked, valueCtx)
		} else {
			values, err = compressWith(masked, valueCtx, comp)
		}
		if err != nil {
			return nil, err
		}
		bitmap, countedNulls, err := array.ValidityBitmap(arr, 0, arr.Length(), ctx.validityBitmapOptions())
		if err != nil {
			return nil, fmt.Errorf("codec: materialize validity: %w", err)
		}
		if countedNulls != nullCount {
			return nil, fmt.Errorf("codec: validity has %d nulls, metadata says %d", countedNulls, nullCount)
		}
		validity, err = compressUnsigned(array.NewPrimitivesUnsafe(bitmap), ctx)
		if err != nil {
			return nil, fmt.Errorf("codec: compress validity: %w", err)
		}
	}
	nullable, err := codec.NewNullable(values, validity, nullCount)
	if err != nil {
		return nil, fmt.Errorf("codec: build nullable: %w", err)
	}
	raw, err := codec.EncodeRaw(arr)
	if err != nil {
		return nil, err
	}
	if raw.BinarySize() <= nullable.BinarySize() {
		return raw, nil
	}
	// A raw values child wraps its source directly, keeping the lazy masked
	// view alive and re-materializing it on every WriteTo; pin one
	// materialized copy instead. Delta and zigzag children hold Virtual views
	// of the lane one level down; that corner stays lazy.
	if values.CodecType() == CodecTypeRaw {
		materialized, err := masked.materialized(ctx.maxBytes)
		if err != nil {
			return nil, fmt.Errorf("codec: materialize nullable values: %w", err)
		}
		values, err = codec.EncodeRaw(materialized)
		if err != nil {
			return nil, fmt.Errorf("codec: wrap nullable values: %w", err)
		}
		nullable, err = codec.NewNullable(values, validity, nullCount)
		if err != nil {
			return nil, fmt.Errorf("codec: rebuild nullable: %w", err)
		}
	}
	return nullable, nil
}

func compressSigned[T array.SignedInteger](arr array.Array[T], ctx planContext) (EncodedArray[T], error) {
	return compressFamilyNullable(arr, ctx, signedIntCompressor[T]{}, maskPrimitiveArray[T], func(values array.ArrayCore[T], childCtx planContext) (EncodedArray[T], error) {
		return codec.EncodeIntegerSparseWithFill(values, childCtx.nullCount, sparseChildren[T]{ctx: childCtx, compressValues: compressSignedCore[T]}, childCtx.buildOptions())
	})
}

func compressUnsigned[T array.UnsignedInteger](arr array.Array[T], ctx planContext) (EncodedArray[T], error) {
	return compressFamilyNullable(arr, ctx, unsignedIntCompressor[T]{}, maskPrimitiveArray[T], func(values array.ArrayCore[T], childCtx planContext) (EncodedArray[T], error) {
		return codec.EncodeIntegerSparseWithFill(values, childCtx.nullCount, sparseChildren[T]{ctx: childCtx, compressValues: compressUnsignedCore[T]}, childCtx.buildOptions())
	})
}

func compressFloat32(arr array.Array[float32], ctx planContext) (EncodedArray[float32], error) {
	return compressFamilyNullable(arr, ctx, float32Compressor(), maskPrimitiveArray[float32], func(values array.ArrayCore[float32], childCtx planContext) (EncodedArray[float32], error) {
		return codec.EncodeFloatSparseWithFill(values, childCtx.nullCount, sparseChildren[float32]{ctx: childCtx, compressValues: compressFloat32Core}, childCtx.buildOptions())
	})
}

func compressFloat64(arr array.Array[float64], ctx planContext) (EncodedArray[float64], error) {
	return compressFamilyNullable(arr, ctx, float64Compressor(), maskPrimitiveArray[float64], func(values array.ArrayCore[float64], childCtx planContext) (EncodedArray[float64], error) {
		return codec.EncodeFloatSparseWithFill(values, childCtx.nullCount, sparseChildren[float64]{ctx: childCtx, compressValues: compressFloat64Core}, childCtx.buildOptions())
	})
}

func compressString(arr array.Array[string], ctx planContext) (EncodedArray[string], error) {
	return compressFamilyNullable(arr, ctx, stringCompressor{}, maskStringArray, func(values array.ArrayCore[string], childCtx planContext) (EncodedArray[string], error) {
		return codec.EncodeStringSparseWithFill(values, childCtx.nullCount, sparseChildren[string]{ctx: childCtx, compressValues: compressStringCore}, childCtx.buildOptions())
	})
}
