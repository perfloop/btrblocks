# btrblocks

A Go library for cascaded columnar compression. Encodes typed arrays of integers, floats, and strings into compact binary representations using layered encoding schemes that compose automatically.

Based on the [BtrBlocks paper](https://db.in.tum.de/~durner/papers/btrblocks.pdf) from TU Munich.

```go
import (
    "github.com/axiomhq/btrblocks"
    "github.com/axiomhq/btrblocks/array"
)

// Compress
arr := array.NewPrimitivesUnsafe([]int32{1, 2, 3, 2, 1, 2, 3, 2})
encoded, err := btrblocks.Compress(arr, btrblocks.Options{})

// Serialize
var buf bytes.Buffer
encoded.WriteTo(&buf)

// Load
loaded, err := btrblocks.Load[int32](buf.Bytes())

// Decompress
values, err := btrblocks.Decompress(loaded)
```

## API

Three functions. That's the public surface.

```go
// Compress encodes an array. The planner picks the best scheme automatically.
func Compress[T Integer | Float | String](arr array.Array[T], opts Options) (EncodedArray[T], error)

// Load deserializes an encoded array from bytes. Zero-copy: the returned
// value may reference the input slice, so keep it alive.
func Load[T Integer | Float | String](data []byte, opts ...ReadOptions) (EncodedArray[T], error)

// Decompress decodes an encoded array into a new slice.
func Decompress[T Integer | Float | String](e EncodedArray[T]) ([]T, error)
```

`EncodedArray[T]` also supports:
- `DecompressInto(dst []T)` — decode into a caller-owned buffer (zero alloc on hot path)
- `ValueAt(offset uint64) T` — random access without full decompression
- `WriteTo(w io.Writer)` — serialize to any writer
- `Slice(start, end uint64)` — logical sub-range

### Options

The zero value of `Options` enables all schemes with a default cascade depth of 3. Use the fluent API to restrict:

```go
// Exclude specific schemes:
opts := btrblocks.Options{}.WithExcludeFloat(btrblocks.CodecTypeALP)

// Allow only specific schemes:
opts := btrblocks.EmptySchemes().WithIncludeInteger(btrblocks.CodecTypeBitpack, btrblocks.CodecTypeFor)

// Limit cascade depth:
opts := btrblocks.Options{}.WithMaxDepth(2)
```

## Encoding schemes

| Scheme | Types | What it does |
|--------|-------|--------------|
| **Raw** | all | Uncompressed. Fallback when nothing beats it. |
| **Const** | all | One repeated value. Header only, no body. |
| **Dict** | all | Unique values + ordinal indices. Indices are recursively compressed. |
| **RunEnd** | all | Run values + boundary positions. Both children are recursively compressed. |
| **Sequence** | int | Arithmetic progression. Stores base + step (2 values). |
| **ZigZag** | signed int | `(n << 1) ^ (n >> 63)` maps signed to unsigned, then compresses the child. |
| **FoR** | unsigned int | Subtract min, bitpack the deltas. Width = `bits.Len(max - min)`. |
| **Bitpack** | unsigned int | Fixed-width bit packing. Outliers go to a sparse patch array. |
| **ALP** | float | Multiply by `10^e / 10^f` to get integers. Exceptions patched. |
| **ALP-RD** | float | Split bit pattern: dictionary on upper bits, bitpack lower bits. |
| **FSST** | string | Fast Static Symbol Table. Byte-level dictionary compression. |

### Cascading

Schemes compose. The planner picks a scheme at each level, up to `MaxDepth`:

```
int32 column: [-5, -5, 3, 3, 3, 7, 7]
  └─ RunEnd (3 runs)
       ├─ runs: [-5, 3, 7] → ZigZag → Bitpack(4-bit)
       └─ ends: [2, 5]     → FoR(min=2) → Bitpack(2-bit)
```

The planner samples ~1% of the data, estimates compression ratios for all applicable schemes, picks the winner, and builds it. If the winner doesn't beat raw, raw is used.

## Wire format

Every node in the compression tree starts with a 24-byte header:

```
Offset  Size  Field
0       1     Version (must be 1)
1       1     CodecType
2       1     PType (element type)
3       1     Reserved (must be 0)
4       4     Flags (codec-specific)
8       8     Length (logical element count)
16      8     BodySize (inline bytes after header, before children)
```

What follows the header depends on `BodySize`:

- **BodySize = 0**: children follow immediately (dict, runend, zigzag)
- **BodySize > 0**: inline data, then children (for: min value; bitpack: width + packed bits; alp: exponents; etc.)

Leaf arrays (raw, const) embed a 20-byte `array.Header` + body inside `BodySize`.

All integers are little-endian. The format requires a little-endian platform (enforced at init).

## Array types

The `array` sub-package provides the input/output types:

```go
// Fixed-width numeric arrays. Zero-copy read, O(1) access.
arr := array.NewPrimitivesUnsafe([]uint32{1, 2, 3})  // borrows slice
arr := array.NewPrimitives([]uint32{1, 2, 3})         // copies slice

// Variable-length strings. Offset type chosen automatically.
arr := array.NewStrings([]string{"hello", "world"})
```

Both implement `array.Array[T]` which provides `ValueAt`, `Length`, `Slice`, `WriteTo`, and `BinarySize`.

## Performance

All numbers from `go test -bench` on Apple M3 Max, 1M-element arrays.

### Decompress throughput

| Codec | Time | Allocs | Memory |
|-------|------|--------|--------|
| Bitpack | 939 us | 1 | 4.0 MB |
| Dict | 1,413 us | 3 | 5.0 MB |
| ALP | 1,396 us | 4 | 9.0 MB |
| ZigZag | 1,389 us | 3 | 5.0 MB |
| RunEnd | 2,065 us | 6 | 10.3 MB |
| ALP-RD | 3,653 us | 1 | 8.0 MB |
| FSST | 85,660 us | 4 | 90 MB |

### Key implementation decisions

**Word-at-a-time bitpack decode.** Single 64-bit load + shift + mask per value instead of byte-at-a-time loop. Covers all practical bit widths up to 56 bits. Last 7 bytes fall back to byte-at-a-time. **2.6x faster** for bitpack; cascades to every compound codec that uses bitpacked children.

**Bulk decompress, not per-element.** `DecompressInto` decodes children into temporary slices then scatters, rather than calling `ValueAt` per element through the codec tree. Per-element dispatch through interface calls + bit extraction is ~50x slower. `ValueAt` exists for random access; hot-path iteration always bulk-decodes.

**Native-typed ordinal scatter.** Dict decompression type-switches on the ordinal width once, decodes into the native slice, and scatters directly. No intermediate `[]uint64` widening. **2.3x faster, 61% less memory.**

**FSST zero-copy strings.** Decode all compressed codes into one buffer, return strings that alias it via `unsafe.String`. One allocation instead of N. **250,000x fewer allocs, 40% less memory.**

**FoR single-pass min/max.** Merged four passes (min, range, histogram, pack) into two (min/max, pack). Skipped histogram since FoR deltas have no exceptions. **21% faster.**

**Per-element ALPRD (intentional).** Batch-unpacking left+right parts was only 6% faster but tripled memory. Kept per-element because the word-at-a-time fast path already handles each unpack in one 64-bit load.

## Ownership and zero-copy

`Load` returns an `EncodedArray` that may alias the input `[]byte`. The caller must keep the input alive for the lifetime of the returned value. This is the same contract as `array.Primitives` constructed via `NewPrimitivesUnsafe`.

`Compress` may return a raw-encoded array that references the input `array.Array`. The input must remain valid.

`Decompress` always returns a new, owned slice.

## Requirements

- Go 1.25+
- Little-endian platform (x86_64, ARM64)
