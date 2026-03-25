# btrblocks

A Go implementation of [BtrBlocks](https://db.in.tum.de/~durner/papers/btrblocks.pdf)-style cascaded columnar encoding for integers, floats, and strings.

## Encoding Schemes

| Scheme | Types | Description |
|--------|-------|-------------|
| Const | all | Single repeated value |
| Raw | all | Uncompressed leaf array |
| Dict | all | Dictionary + ordinal indices |
| RunEnd | all | Run-length with encoded run values and end boundaries |
| Sequence | int | Arithmetic progression (base + step) |
| ZigZag | signed int | Maps signed to unsigned via zigzag transform, then encodes child |
| FoR | unsigned int | Frame-of-Reference: subtract min, then bitpack delta |
| Bitpack | unsigned int | Fixed-width bit-packed values with optional patches |
| ALP | float | Adaptive Lossless floating-Point: multiply by 10^e / 10^f to integer |
| ALP-RD | float | ALP for Real Doubles: dictionary on upper bits, bitpack lower bits |
| FSST | string | Fast Static Symbol Table compression |

Schemes cascade: a dict's indices may be bitpacked, which may use FoR, etc. A planner samples ~1% of the data, estimates compression ratios, and picks the best scheme at each level up to a configurable depth.

## Performance Decisions

This section documents the key decisions made to optimize decode (decompress) throughput and memory usage. All numbers are from `go test -bench` on Apple M3 Max, 1M-element arrays.

### 1. Word-at-a-time bitpack decode

**Problem:** The original `unpackUnsigned` extracted values one byte-span at a time, looping 2-4 iterations per value for typical bit widths (3-8 bits). Since bitpacking underlies dict indices, FoR deltas, zigzag children, and ALP integer children, this was the hottest inner loop in the system.

**Solution:** Read a 64-bit word from the buffer at the value's byte offset and extract with a single shift+mask. This works for any `bitWidth + alignment_offset <= 64` (covers all practical cases up to 56-bit values). The last 7 bytes of the buffer fall back to byte-at-a-time to avoid out-of-bounds reads.

A batch variant (`unpackBatchTyped`) hoists the mask out of the loop and writes directly into the caller's typed slice, eliminating per-element function call overhead.

**Impact:** Bitpack 1M: 2,420us -> 939us (**2.6x faster**). Cascades to every compound codec.

### 2. Native-typed ordinal scatter (dict decompress)

**Problem:** `dictArray.DecompressInto` called `decompressOrdinals` which: (a) decompressed indices into their native type (e.g. `[]uint8`), then (b) widened every element into a new `[]uint64` slice. For a 1M-row dict column with uint8 ordinals, the widening allocated 8MB that was immediately iterated and discarded.

**Solution:** `scatterOrdinals` type-switches on the ordinal view once, decompresses into the native typed slice, and scatters `values[idx]` directly into `dst`. The `[]uint64` widening allocation is eliminated entirely.

**Impact:** Dict 1M: 13MB -> 5MB allocs (**61% less memory**), 3,230us -> 1,413us (**2.3x faster** combined with bitpack improvement).

### 3. Per-element ValueAt vs bulk decompress

**Decision:** During iteration on dict and runend, we benchmarked replacing bulk `Decompress()` + scatter with per-element `ValueAt()` calls through the codec tree. This was a **50x regression** for RunEnd (3.1ms -> 158ms) because virtual dispatch + bit extraction per element through the codec tree dominates. Bulk decompress in a tight loop with no per-element dispatch is fundamentally faster for sequential access.

**Rule:** Use bulk decompress for sequential scans. Reserve `ValueAt` for random access (point queries, patch lookup). Never replace a bulk decompress loop with per-element `ValueAt` on the hot path.

### 4. Batch unpack vs per-element unpack for ALPRD

**Problem:** After the word-at-a-time `unpackUnsigned` optimization (decision 1), ALPRD still calls `unpackUnsigned` twice per element (left codes + right parts). We benchmarked batch-unpacking both into `[]uint64` scratch buffers, then assembling floats in a separate loop.

**Result:** Only ~6% faster (3.7ms -> 3.5ms) but triples memory usage (8MB -> 24MB for 1M elements) due to two N*8-byte scratch allocations. The word-at-a-time fast path already captures most of the gain since each `unpackUnsigned` call does a single 64-bit load + shift + mask.

**Decision:** Keep per-element `unpackUnsigned` for ALPRD. The memory cost of scratch buffers is not justified by a 6% speed improvement. Document the tradeoff in the code so future optimizers don't re-run this experiment.

### 5. FoR multi-pass reduction

**Problem:** `buildFoRArray` made 4 passes over the input array: (1) find min, (2) find range width, (3) histogram in `unsignedBitWidthHistogram`, (4) pack + collect patches. Each pass called `arr.ValueAt` N times, and through `forEncodedArray` that includes a subtraction per call.

**Solution:** Merge passes 1+2 into a single min/max scan. Skip pass 3 entirely by passing the known bit width directly to `buildBitPackedArrayWithWidth` — FoR deltas are always in `[0, max-min]` with no exceptions, so the histogram-based width selection is unnecessary.

**Impact:** FoR compress 1M: 23,350us -> 18,479us (**21% faster**). Reduces from 4N to 2N element reads.

### 6. BodySize header contract

Each encoded array has a 24-byte header with a `BodySize` field. The contract:

- **BodySize = bytes of inline data written directly after the header, before any recursive EncodedArray children or patch streams.**
- Leaf-wrapping codecs (raw, const): BodySize = the wrapped `array.Array`'s full binary size.
- Codecs with recursive children (dict, runend, zigzag): BodySize = 0.
- Codecs with fixed inline fields + children (for, bitpack, alp, alprd, fsst, sequence): BodySize = inline portion only.

All readers validate BodySize against expected values during deserialization.

### 7. Immutable stats flow

Compressor structs carry no mutable state. The `Schemes(stats)` method receives pre-computed statistics (including distinct-value maps for dict encoding) as a parameter. Build closures capture from the immutable stats value, not from shared mutable fields on the compressor. This eliminates a class of stale-data bugs and makes concurrent compression safe.

### 8. FSST zero-copy string decompress

**Problem:** `fsstArray.DecompressInto` called `string(decoded[pos:pos+l])` for each element, which copies the bytes into a new heap-allocated string. For 1M strings this produced 1,000,006 allocations and 150MB of memory.

**Solution:** Decode all compressed codes into a single contiguous buffer via `table.DecodeAll(f.codes)` (one allocation), then use `unsafe.String(unsafe.SliceData(buf), len(buf))` to create strings that alias directly into the decoded buffer. This reduces allocations from O(N) to O(1).

**Ownership contract:** The returned strings alias the decoded buffer. They remain valid as long as any string in the output slice is reachable — Go's GC traces the `unsafe.Pointer` in the string header back to the decoded slice. This is the same aliasing contract as `array.Strings.ValueAt`.

**Impact:** FSST 1M: 102,500us -> 85,660us (**16% faster**), 1,000,006 -> 4 allocs (**250,000x fewer**), 150MB -> 90MB (**40% less memory**).

## Benchmark Summary

Decompress throughput on 1M elements (Apple M3 Max):

| Codec | Throughput | Allocs | Bytes/op |
|-------|-----------|--------|----------|
| Bitpack | 939 us | 1 | 4.0 MB |
| Dict | 1,413 us | 3 | 5.0 MB |
| ALP | 1,396 us | 4 | 9.0 MB |
| ZigZag | 1,389 us | 3 | 5.0 MB |
| RunEnd | 2,065 us | 6 | 10.3 MB |
| ALP-RD | 3,653 us | 1 | 8.0 MB |
| FSST | 85,660 us | 4 | 90 MB |
