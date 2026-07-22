# btrblocks

A Go implementation of [BtrBlocks](https://www.cs.cit.tum.de/dis/research/btrblocks/)-style
cascading compression for columnar data.

A sampling planner picks the best encoding per array — and encodings recurse:
a dictionary's index column may itself be run-end encoded, a delta stream's
residuals bitpacked. Arrays are nullable natively: validity travels as a
bitmap beside the values, every encoding supports null rows, and all-valid
arrays pay zero bytes for it.

## Packages

- `github.com/axiomhq/btrblocks` — the convenience API for selecting,
  loading, and decompressing encodings.
- `github.com/axiomhq/btrblocks/compress` — sampling, statistics, estimates,
  exclusions, and recursive codec selection.
- `github.com/axiomhq/btrblocks/codec` — typed wire-format nodes and low-level
  builders: FoR+bitpacking, delta, zigzag, dictionary, run-end, sparse,
  sequence, constant, ALP/ALP-RD, FSST, nullable, and raw.
- `github.com/axiomhq/btrblocks/array` — the physical layer: fixed-width
  primitive arrays, string arrays, validity bitmaps, and their wire format.

## Usage

```go
import (
	"github.com/axiomhq/btrblocks"
	"github.com/axiomhq/btrblocks/array"
)

// Build an array and compress it.
values := array.NewPrimitives([]int64{10, 12, 14, 16, 18})
encoded, err := btrblocks.SignedArray(values, btrblocks.Options{})

// Serialize into caller-owned bytes without decompressing.
data, err := encoded.MarshalBinary()

// Load without decompressing.
decoded, err := btrblocks.LoadSigned[int64](data)

// Random access, or bulk decompression.
v := decoded.ValueAt(2)
out := make([]int64, decoded.Length())
err = decoded.DecompressInto(out)
```

`MarshalBinary` allocates `BinarySize()` contiguous bytes. For large outputs,
use `WriteTo` to stream the same wire representation to an `io.Writer` without
that allocation.

Nulls are passed alongside values; `true` marks a null row and an empty mask
means that every row is valid:

```go
arr, err := array.NewPrimitivesWithNulls(
    []int64{7, 0, 9, 11},
    []bool{false, true, false, false},
)
encoded, err := btrblocks.SignedArray(arr, btrblocks.Options{})
// encoded.IsValid(1) == false
```

`UnsignedArray`, `Float32Array`, `Float64Array`, and `StringArray` mirror
`SignedArray`; `LoadUnsigned`, `LoadFloat32`, `LoadFloat64`, and
`LoadStrings` mirror `LoadSigned`. Loading is validating: untrusted bytes are
rejected with errors, never panics, and `ReadOptions` bounds decode-time
allocations. Compression similarly limits individual temporary
materializations to 64 MiB by default; raise that limit explicitly with
`Options{}.WithMaxBytes(n)` for larger columns.

## Production support

The production targets are Linux on amd64 and arm64. Both run the full test
suite in CI; amd64 additionally runs the race detector and scheduled fuzzing.
The zero-copy physical-array implementation requires a little-endian target,
which is enforced by its build constraints. Persisted or network-provided data
should always be loaded with explicit `ReadOptions` chosen for the workload.

## Wire format

Wire format version 1 is a pre-release draft. Until the first `v1.0.0` tag,
layouts and enum values may change and stored bytes may need to be rewritten.
The first release freezes its exact version 1 format; later incompatible
writers must use a new version. See [FORMAT.md](FORMAT.md) for the byte-level
draft and [RELEASING.md](RELEASING.md) for the release gates.

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
