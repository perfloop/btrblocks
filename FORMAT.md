# BtrBlocks Go wire format

## Pre-release compatibility policy

Wire format version 1 is a draft until the first `v1.0.0` tag. Before that
release, layouts, flags, and enum values may change without a migration path;
stored bytes may need to be rewritten. The current golden tests are regression
checks, not a backward-compatibility promise.

The first release freezes the exact version 1 layout. From that tag onward, a
valid version 1 stream written by a released writer must remain readable;
incompatible changes require a new format version, and frozen enum/flag values
become append-only. Unknown versions, types, codecs, reserved bytes, and flags
are rejected today so that evolution remains explicit.

Streams intentionally have no magic prefix. Callers enter the decoder at a
known array or codec-node boundary. Applications embedding these streams in a
larger file or protocol should provide their own outer framing, magic, and
checksums.

All multibyte integers are little-endian. The production implementation
supports Linux amd64 and arm64. Decoder results may borrow the input byte slice;
the slice must remain alive and unchanged while the result is in use.

## Physical arrays

Every physical array begins with this 20-byte header:

| Offset | Size | Field |
|---:|---:|---|
| 0 | 1 | version (`1`) |
| 1 | 1 | physical type |
| 2 | 2 | flags |
| 4 | 8 | logical row count |
| 12 | 8 | body byte count |

Flag bit 0 means the body starts with a validity bitmap. Bits are LSB-first;
one means valid, zero means null, and unused high bits in the final byte are
zero. The bitmap occupies `ceil(length/8)` bytes. No bitmap is written for an
all-valid array.

Numeric bodies contain `length` fixed-width values after the optional bitmap.
String bodies contain an optional bitmap, a `uint32` byte-buffer length,
`length+1` monotonic offsets, and the string bytes. Offset width is 1, 2, 4,
or 8 bytes and is inferred from the declared body size. The first offset is
zero and the final offset equals the byte-buffer length.

Current physical type values:

| Value | Type | Value | Type |
|---:|---|---:|---|
| 0 | unknown | 6 | uint16 |
| 1 | int8 | 7 | uint32 |
| 2 | int16 | 8 | uint64 |
| 3 | int32 | 9 | float32 |
| 4 | int64 | 10 | float64 |
| 5 | uint8 | 11 | string |

## Codec nodes

Every codec-tree node begins with this 24-byte header:

| Offset | Size | Field |
|---:|---:|---|
| 0 | 1 | version (`1`) |
| 1 | 1 | codec type |
| 2 | 1 | physical element type |
| 3 | 1 | reserved (`0`) |
| 4 | 4 | codec flags |
| 8 | 8 | logical row count |
| 16 | 8 | inline body byte count |

The inline body is followed by zero or more complete child codec nodes. The
node type determines child order and interpretation:

| Value | Codec | Inline body and children |
|---:|---|---|
| 1 | const | one physical-array body |
| 2 | raw | one physical array |
| 3 | dictionary | values child, ordinal child |
| 4 | run-end | runs child, end-offset child |
| 5 | zigzag | unsigned child |
| 6 | bitpack | bit width, packed bytes, optional patches |
| 7 | frame-of-reference | minimum value, residual child |
| 8 | sparse | fill, indices, values; bitmap form omits indices |
| 9 | sequence | base and step |
| 10 | ALP | exponent pair, encoded child, optional patches |
| 11 | FSST | table and code buffers, offsets child, lengths child |
| 12 | ALP-RD | bit widths, dictionary, packed buffers, optional patches |
| 13 | delta | base value, delta child |
| 14 | nullable | null count, values child, validity-byte child |

Patch payloads contain an eight-byte logical offset followed by an index child
and a value child. Codec-specific readers validate exact body sizes, child
types and lengths, flags, offsets, ordering, and decoded-size limits before a
node is accepted.

The literal tests in `array/header_test.go` and `codec/wire_test.go` guard the
current version 1 framing. Before `v1.0.0`, intentional format changes update
both these fixtures and this document; the release tag turns them into
compatibility sentinels.
