# btrblocks

Go implementation of BtrBlocks-style cascaded lightweight compression for columnar data.

### Trust the encoder

Encoder invariants (valid dict indices, monotonic run-end offsets, correct child counts) are guaranteed by construction. The decode path does **not** re-validate them. This matches Vortex's `new_unchecked` approach.

- Dict indices are never bounds-checked during decode. The encoder builds the dictionary and indices together — an out-of-range index cannot be produced.
- Run-end offsets are never checked for monotonicity or range during decode. The encoder constructs them from a forward scan.
- Out-of-range access from hand-crafted or corrupted codecs will panic (slice bounds), not return an error.

The read path (deserialization) performs only **O(1) structural checks**: version, flags, child count, body size, length consistency. No per-element scans.

### Always bulk-decode

Nested codecs always bulk-decode their children via `Decode()`. There is no `ValueAt` fallback path and no scratch budget. This keeps the decode path simple and fast.

- `DictCodec.Decode`: bulk-decodes values table + indices, then does a direct table lookup.
- `RunendCodec.Decode`: bulk-decodes runs + ends, then fills in a single forward pass.
- `ZigzagCodec.Decode`: bulk-decodes the unsigned child, then zigzag-decodes in place.

Callers needing bounded memory should use `Read()` to get a `Codec`, then call `ValueAt()` for random access without materializing.

### Width narrowing

All codecs that store auxiliary arrays (dict indices, run-end offsets, zigzag-encoded values) select the narrowest unsigned integer type that fits:

- `uint8` if max value fits in 8 bits
- `uint16` if max value fits in 16 bits
- `uint32` if max value fits in 32 bits
- `uint64` otherwise

This directly reduces wire size and decode-time memory.

### Cascaded compression

Codecs compress their children recursively up to a configurable depth (default 3). The `selectBest` function tries all applicable codecs and picks the one with the smallest `BinarySize()`. For example, dict indices might be further compressed via bitpacking, and run-end values via const encoding.

## What this library does NOT do

- **No per-element validation on decode.** If you deserialize untrusted data, corrupt indices will panic, not error. Validate at your system boundary before decoding.
- **No memory budgeting during decode.** `Decode()` allocates scratch arrays proportional to the child codec lengths. For very large columns, use `ValueAt()` for bounded-memory random access.
- **No streaming decode.** `Decode()` materializes the full output. There is no iterator or chunk-based API.
- **No concurrency.** Codecs are not safe for concurrent use. The caller is responsible for synchronization.
- **No SIMD or vectorized decode.** The decode loops are scalar Go. The design preserves options for future SIMD work (contiguous buffers, no branching in hot loops).

## Wire Format

Every codec node is:

```
[Header (24 bytes)][Body (BodySize bytes)][Child codec nodes (ChildCount children)]
```

**Codec Header (24 bytes):**
| Field | Size | Description |
|-------|------|-------------|
| Version | 1 | Format version (currently 1) |
| Kind | 1 | Codec type (const, raw, dict, runend, zigzag, bitpacking) |
| ElemType | 1 | Physical element type (int8..uint64, float32, float64, string) |
| ChildCount | 1 | Number of child codec nodes following the body |
| Flags | 4 | Reserved (must be 0) |
| Length | 8 | Number of logical elements |
| BodySize | 8 | Size in bytes of the codec-local body |

**Array Header (20 bytes):**
| Field | Size | Description |
|-------|------|-------------|
| Version | 1 | Format version (currently 1) |
| PType | 1 | Physical element type |
| Flags | 2 | Reserved (must be 0) |
| Length | 8 | Number of elements |
| BodySize | 8 | Size in bytes of the array body |

All integers are little-endian. No magic bytes — codec streams are only entered through typed decode paths.

## Codec Types

| Codec | Kind | Children | Description |
|-------|------|----------|-------------|
| Raw | 2 | 0 | Uncompressed array |
| Const | 1 | 0 | Single repeated value |
| Dict | 3 | 2 (values, indices) | Dictionary compression |
| Runend | 4 | 2 (runs, ends) | Run-length encoding |
| Zigzag | 5 | 1 (unsigned data) | Signed-to-unsigned via zigzag encoding |
| Bitpacking | 6 | 0 | Variable bit-width packing for unsigned integers |

## Package Structure

- `btrblocks/` — Codec implementations, compression pipeline, wire format
- `btrblocks/array/` — Physical array types (primitives, strings), serialization
