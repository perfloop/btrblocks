# Changelog

## Unreleased

- Split physical arrays, codec nodes, and compression selection into public
  `array`, `codec`, and `compress` packages while retaining the root facade.
- Added first-class null-mask constructors and bounded validity materialization.
- Added build and decode memory limits, recursive-depth limits, hostile-input
  tests, version 1 framing fixtures, and production CI gates.
- Added a whole-decode validation-work budget: `ReadOptions.MaxWork` (default
  `DefaultMaxWork`) caps the total bytes materialized across a decode's
  validation scans — `MaxDecodedBytes` bounds one scan, `MaxWork` bounds their
  sum — so a small crafted stream cannot force a large decode. Exhaustion
  returns `ErrWorkLimit`. The budget derives only from the stream and the
  options, never from the size of the caller's buffer, so the same bytes and
  options always produce the same accept/reject decision.
- Bounded run-end and sparse child sizes at header-read time: a node rejects an
  ordinal, index, or value child that declares more elements than its row count
  and index type can hold before that child's subtree is decoded.
- Added `EncodedArray.MarshalBinary` for caller-owned wire bytes without
  decompression; `WriteTo` remains the streaming path for large outputs.
- Documented the draft wire format and the compatibility boundary established
  by the first `v1.0.0` release.

### Breaking changes

No prior release exists; these change the public API relative to earlier
pre-1.0 development of the default branch.

- Renamed `compress.Options.MaxBuildBytes` to `MaxBytes` and
  `compress.Options.WithMaxBuildBytes` to `WithMaxBytes`.
- Removed `array.ValidityBitmapOptions` and `array.DefaultMaxValidityBitmapBytes`;
  `array.ValidityBitmap` now takes the shared `BuildOptions`, so validity
  materialization is bounded by the same build byte budget as everything else.
- Removed `codec.ALPRDChildBuilder`; `EncodeALPRD32` and `EncodeALPRD64` take the
  shared `codec.UnsignedChildBuilder`.
- Removed the duplicate codec vocabulary that `compress` re-exported; reference
  the `codec` names (`codec.CodecType…`, `codec.DefaultMaxBuildBytes`, the codec
  error sentinels) directly.
