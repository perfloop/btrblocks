// Package btrblocks provides a convenience facade for building and reading
// BtrBlocks-style encodings for columnar arrays. A sampling selector picks the
// best scheme per array
// from FoR+bitpacking, delta, zigzag, dictionary, run-end, sparse, sequence,
// constant, ALP/ALP-RD (floats), FSST (strings), and raw. Validity is
// preserved independently from physical values, so every encoding supports
// nullable input even when a particular value scheme does not. Encoded
// arrays support random access, bulk decompression, slicing, and
// serialization. Selection policy lives in the compress subpackage;
// wire-format nodes and low-level builders live in codec.
package btrblocks
