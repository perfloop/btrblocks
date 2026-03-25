# Design

This document describes the internal architecture. Read the README first for the public API.

## Architecture

The system has four layers:

```
┌─────────────────────────────────────────────┐
│  Public API: Compress, Load, Decompress     │
├─────────────────────────────────────────────┤
│  Planner: stats, sampling, scheme selection │
├─────────────────────────────────────────────┤
│  Codecs: encode, decode, serialize          │
├─────────────────────────────────────────────┤
│  Arrays: typed columnar storage + I/O       │
└─────────────────────────────────────────────┘
```

Data flows down through the planner into codecs during compression, and up from codecs into arrays during decompression. The planner never touches serialization; codecs never make planning decisions.

## Arrays

The `array` sub-package defines two interfaces:

```go
ArrayCore[T]  // ValueAt + Length (read-only scan)
Array[T]      // ArrayCore + WriteTo + CopyTo + Slice + BinarySize + PType
```

Two concrete types implement `Array[T]`:

- `Primitives[T]` — fixed-width numerics backed by a `[]T` slice. `ValueAt` is a direct index. `WriteTo` casts the backing slice to `[]byte` via `unsafe.Slice` and writes it in one call. `readPrimitivesFromBuf` does the reverse: it takes a subslice of the `BufReader`'s buffer and reinterprets it as `[]T` with no copy.

- `Strings[T]` — variable-length strings with a typed offset array `[]T` (T is uint8/uint16/uint32, chosen by total byte length) and a contiguous `[]byte` buffer. `ValueAt` returns `unsafe.String` aliasing the buffer. The offset type is chosen at construction time and inferred from the binary format during deserialization.

### Binary format (arrays)

```
┌──────────────────────────────────────────────┐
│ Header (20 bytes)                            │
│   Version(1) PType(1) Flags(2)               │
│   Length(8) BodySize(8)                      │
├──────────────────────────────────────────────┤
│ Body (BodySize bytes)                        │
│   Primitives: raw element bytes              │
│   Strings: bufLen(4) + offsets + string data │
└──────────────────────────────────────────────┘
```

All integers are little-endian. Platform endianness is checked at init.

## Codecs

Each encoding scheme is a struct implementing `EncodedArray[T]`:

```go
type EncodedArray[T] interface {
    Encoding() CodeType        // which codec
    ValueAt(offset uint64) T   // O(1) random access
    DecompressInto(dst []T)    // bulk decode
    Slice(start, end uint64)   // logical sub-range
    BinarySize() uint64        // serialized size
    Length() uint64
    PType() PType
    WriteTo(w io.Writer)       // serialize
}
```

Codecs fall into three structural categories:

### Leaf codecs

**Raw** wraps an `array.Array[T]` with no transformation. It exists so that every value in the system is an `EncodedArray`, even uncompressed data.

**Const** stores a single value and a length. `DecompressInto` fills the destination using copy-doubling (`fillRun`).

**Sequence** stores base + step. No array body at all.

### Delegating codecs

These have zero inline body (`BodySize = 0`). Their children follow immediately after the header.

**Dict** stores two children: `values` (unique elements, recursively compressed) and `indices` (ordinals into values, recursively compressed). Decompression bulk-decodes both children, then scatters `values[indices[i]]` into the destination.

**RunEnd** stores `runs` (run values) and `ends` (boundary positions). Both are recursively compressed. Decompression bulk-decodes both, then fills spans using copy-doubling.

**ZigZag** stores a single unsigned child. Encoding maps `n → (n << 1) ^ (n >> 63)`. The child type is chosen by the max encoded value (uint8/16/32/64).

### Inline + child codecs

These have a fixed inline body followed by one or more children and optional patches.

**FoR** (Frame-of-Reference) stores `min` (inline, 1-8 bytes) + a bitpacked child. All values are stored as `value - min`. The child's bit width is `bits.Len(max - min)`.

**Bitpack** stores `bitWidth` (1 byte) + packed bits (inline). Outliers exceeding the bit width are stored in a sparse patch array. Width is chosen by histogram analysis to minimize `packed_cost + exception_cost`.

**ALP** stores exponents `e, f` (2 bytes inline) + a signed integer child (recursively compressed) + optional float patches. Each value is encoded as `round(value * 10^e * 10^-f)` and decoded as `encoded * 10^-e * 10^f`. Values that don't round-trip exactly become patches.

**ALP-RD** stores a small dictionary (up to 8 entries) of frequent upper-bit patterns + bitpacked left codes + bitpacked right parts (all inline) + optional patches. No recursive children.

**FSST** stores a serialized symbol table + compressed byte codes (inline) + two recursive children for offsets and original lengths. Offset and length widths are chosen independently: offsets are sized by total compressed bytes, lengths by max original string length. This avoids wasting space when strings are short but numerous.

### Patches

Bitpack, ALP, and ALP-RD use a shared `patches[V, I]` type for sparse exceptions:

```go
type patches[V, I] struct {
    length  uint64           // logical array length
    offset  uint64           // base offset for indices
    indices EncodedArray[I]  // sorted positions
    values  EncodedArray[V]  // override values
}
```

Patches are validated on construction (sorted, in-bounds, non-empty). `Apply` bulk-decompresses both children and scatters into the destination. `Iterate` bulk-decompresses and calls a callback with offset-adjusted positions — used by ALPRD where patches override the left part rather than the final value. `Find` uses binary search for point queries. `Slice` uses binary search on sorted indices to find the range boundaries in O(log N).

## Planner

Compression is a two-phase process: estimate, then build.

### Phase 1: Statistics

Each type family has a compressor that computes stats in one pass:

```
signedIntCompressor   → signedStats   (isConst, distinctCount, avgRunLength, min, max, hasNegative, distinct map)
unsignedIntCompressor → unsignedStats (isConst, distinctCount, avgRunLength, min, max, distinct map)
floatCompressor       → floatStats    (isConst, distinctCount, avgRunLength, distinct map by bit pattern)
stringCompressor      → stringStats   (isConst, avgRunLength, estimatedDistinctCount via prefix hash)
```

Stats are immutable values. `isConst` is derived from `runs == 1` (all adjacent pairs equal), which is correct regardless of distinct map state. The distinct map is retained for dict construction and nil'd when cardinality exceeds `n/2` to bound memory.

### Phase 2: Scheme selection

`chooseScheme` evaluates every registered scheme's `Estimate` function with the pre-computed stats. Estimates return a compression ratio (higher is better). The winner's `Build` function is called with the full array.

For expensive estimators (RunEnd, ZigZag, ALP, ALPRD, FSST), estimation runs on a ~1% stratified sample. Sampling extracts chunks of 64 elements from randomly-selected positions across evenly-distributed partitions. The sample is stored as a `sampledArray` — a chunked virtual array implementing `ArrayCore[T]` with O(log K) binary-search access.

If the winning scheme's encoded size exceeds raw size, raw is used.

### Recursion

Codecs with children (dict, runend, zigzag, for, alp, fsst) recursively call `compressArray` on their child arrays. A `planContext` tracks:

- **Depth** — decremented at each level, stops recursion at zero.
- **Excludes** — per-domain (integer/float/string) bitmask of schemes to skip. Used to prevent cycles (e.g., dict indices exclude dict) and overly deep nesting (e.g., zigzag excludes dict and runend on its child).
- **Sample flag** — set when operating on sampled data, prevents const detection on partial data.

## Serialization

### Encoded array wire format

```
┌──────────────────────────────────────────────┐
│ Codec Header (24 bytes)                      │
│   Version(1) CodecType(1) PType(1) Rsv(1)   │
│   Flags(4) Length(8) BodySize(8)             │
├──────────────────────────────────────────────┤
│ Inline body (BodySize bytes)                 │
│   (varies by codec — see below)              │
├──────────────────────────────────────────────┤
│ Children (recursive EncodedArray streams)    │
├──────────────────────────────────────────────┤
│ Patches (if flagged)                         │
│   offset(8) + indices child + values child   │
└──────────────────────────────────────────────┘
```

The `BodySize` contract is critical:

| Category | BodySize equals | Examples |
|----------|----------------|---------|
| Leaf-wrapping | wrapped Array's full BinarySize | raw, const |
| Delegating | 0 | dict, runend, zigzag |
| Inline + children | inline portion only | for, bitpack, alp, alprd, fsst, sequence |

Readers validate `BodySize` against expected values and reject mismatches.

### Deserialization

`Load` wraps the input `[]byte` in a `BufReader` — a cursor that returns subslices with no allocation. Reading advances the cursor. Each `readXxxArray` function reads its inline body, then recursively calls `readEncodedArray` for children. The returned `EncodedArray` may alias the input buffer (zero-copy for raw arrays and bitpack buffers).

## Type dispatch

Go's generics don't support method-level type parameters, so functions constrained on `Integer | Float | String` must dispatch to concrete types via type switches. This pattern appears in:

- `compressArray` — dispatches to type-specific compressors
- `buildArray` — constructs the correct `Primitives` or `Strings`
- `readAny*Array` — dispatches from generic `readEncodedArrayWithHeader` to concrete readers
- `buildALPArray`, `buildALPRDArray` — dispatch to width-specific builders

The type switches are compile-time exhaustive (the `default` case is unreachable for valid type constraints) and appear only at dispatch boundaries, never in inner loops.

Two cast helpers reduce the boilerplate to one line per arm: `readCast[T](result any, err error)` for `EncodedArray[T]` returns, and `arrayCast[T](result any)` for `array.Array[T]` returns. Shared integer serialization helpers `writeIntegerLE[T]` and `readIntegerLE[T]` consolidate width-switch encoding/decoding used by sequence and FoR codecs.

## File organization

One source file per concern, one test file per source file:

```
compress.go          — public API (Compress, Load, Decompress)
codec.go             — EncodedArray interface, header, dispatch, shared helpers
                       (readCast, arrayCast, writeIntegerLE, readIntegerLE, putLittleEndian)
types.go             — type aliases, CodeType enum, Options
planner.go           — scheme/compressor interfaces, chooseScheme
sample.go            — sampling, sampledArray, buildArray, arrayCast
stats.go             — baseStats
stats_{int,uint,float,str}.go — type-specific stats
compress_{int,uint,float,str}.go — type-specific compressor registrations
codec_{name}.go      — one file per encoding scheme
patches.go           — sparse exception storage

array/
  array.go           — ArrayCore, Array interfaces
  primitive.go       — Primitives[T]
  string.go          — Strings[T]
  header.go          — binary header format
  bufreader.go       — zero-copy cursor
  type.go            — PType enum
```

## Invariants

1. Every `EncodedArray` returned by `Compress` or `Load` is safe to call `DecompressInto`, `ValueAt`, `WriteTo`, and `Slice` on without further setup.

2. `WriteTo` followed by `Load` produces an `EncodedArray` that decodes to the same values. This is tested by round-trip fuzz tests for every codec.

3. `BodySize` in the codec header exactly equals the number of bytes between the header and the first child or end-of-stream. Readers validate this.

4. Stats are computed once and passed as immutable values. No mutable state is shared between the planner and codec build functions.

5. Recursive compression always decrements depth and adds exclusions to prevent cycles. The maximum tree depth is bounded by `Options.MaxDepth` (default 3).

6. Patch indices are validated as sorted, in-bounds, and non-empty on construction. This is checked during both build and deserialization.
