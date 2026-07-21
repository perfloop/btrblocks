package btrblocks

import (
	"github.com/axiomhq/btrblocks/array"
	"github.com/axiomhq/btrblocks/compress"
)

// Options controls codec-tree selection.
type Options = compress.Options

// SignedArray encodes a signed integer array according to opts. Raw fallback
// may continue to reference arr after SignedArray returns.
func SignedArray[T array.SignedInteger](arr array.Array[T], opts Options) (EncodedArray[T], error) {
	return compress.SignedArray(arr, opts)
}

// UnsignedArray encodes an unsigned integer array according to opts. Raw
// fallback may continue to reference arr after UnsignedArray returns.
func UnsignedArray[T array.UnsignedInteger](arr array.Array[T], opts Options) (EncodedArray[T], error) {
	return compress.UnsignedArray(arr, opts)
}

// Float32Array encodes a float32 array according to opts. Raw fallback may
// continue to reference arr after Float32Array returns.
func Float32Array(arr array.Array[float32], opts Options) (EncodedArray[float32], error) {
	return compress.Float32Array(arr, opts)
}

// Float64Array encodes a float64 array according to opts. Raw fallback may
// continue to reference arr after Float64Array returns.
func Float64Array(arr array.Array[float64], opts Options) (EncodedArray[float64], error) {
	return compress.Float64Array(arr, opts)
}

// StringArray encodes a string array according to opts. Raw fallback may
// continue to reference arr after StringArray returns.
func StringArray(arr array.Array[string], opts Options) (EncodedArray[string], error) {
	return compress.StringArray(arr, opts)
}
