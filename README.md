# btrblocks

A Go implementation of [BtrBlocks](https://www.cs.cit.tum.de/dis/research/btrblocks/)-style
cascading compression for columnar data.

A sampling planner picks the best encoding per array — and encodings recurse:
a dictionary's index column may itself be run-end encoded, a delta stream's
residuals bitpacked. Arrays are nullable natively: validity travels as a
bitmap beside the values, every encoding supports null rows, and all-valid
arrays pay zero bytes for it.

## Packages

- `github.com/axiomhq/btrblocks` — the planner and encodings: FoR+bitpacking,
  delta, zigzag, dictionary, run-end, sparse, sequence, constant, ALP and
  ALP-RD for floats, FSST for strings, nullable, and raw fallback.
- `github.com/axiomhq/btrblocks/array` — the physical layer: fixed-width
  primitive arrays, string arrays, validity bitmaps, and their wire format.

## Usage

```go
import (
    "bytes"

    "github.com/axiomhq/btrblocks"
    "github.com/axiomhq/btrblocks/array"
)

// Build an array and compress it.
values := array.NewPrimitives([]int64{10, 12, 14, 16, 18})
encoded, err := btrblocks.SignedArray(values, btrblocks.Options{})

// Serialize.
var buf bytes.Buffer
_, err = encoded.WriteTo(&buf)

// Load without decompressing.
decoded, err := btrblocks.LoadSigned[int64](buf.Bytes())

// Random access, or bulk decompression.
v := decoded.ValueAt(2)
out := make([]int64, decoded.Length())
err = decoded.DecompressInto(out)
```

Nullable input pairs values with a validity bitmap:

```go
validity, err := array.ValidityFromNulls(4, []bool{false, true, false, false})
arr, err := array.NewPrimitivesWithValidity([]int64{7, 0, 9, 11}, validity)
encoded, err := btrblocks.SignedArray(arr, btrblocks.Options{})
// encoded.IsValid(1) == false
```

`UnsignedArray`, `Float32Array`, `Float64Array`, and `StringArray` mirror
`SignedArray`; `LoadUnsigned`, `LoadFloat32`, `LoadFloat64`, and
`LoadStrings` mirror `LoadSigned`. Loading is validating: untrusted bytes are
rejected with errors, never panics, and `ReadOptions` bounds decode-time
allocations.

## Wire format

All headers carry format version 1. The format is not yet frozen — treat it
as unstable until a v1.0.0 tag exists. Do not store bytes you cannot afford
to rewrite.

## Related projects

- [btrblocks](https://github.com/maxi-k/btrblocks) — the reference C++
  implementation from TUM.
- [Vortex](https://github.com/vortex-data/vortex) — an extensible columnar
  format in Rust building on the same line of work; its `vortex-btrblocks`
  crate is an independent BtrBlocks-style compressor.

## References

This is an independent Go implementation of techniques from:

- Kuschewski et al., *BtrBlocks: Efficient Columnar Compression for Data
  Lakes* (SIGMOD 2023)
- Afroozeh, Kuffo, Boncz, *ALP: Adaptive Lossless floating-Point
  Compression* (SIGMOD 2024)
- Boncz, Neumann, Leis, *FSST: Fast Random Access String Compression*
  (VLDB 2020)
