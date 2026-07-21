// Package btrblocks selects, builds, and reads BtrBlocks-style encodings
// for columnar arrays. A sampling planner picks the best scheme per array
// from FoR+bitpacking, delta, zigzag, dictionary, run-end, sparse, sequence,
// constant, ALP/ALP-RD (floats), FSST (strings), and raw. Validity is
// preserved independently from physical values, so every encoding supports
// nullable input even when a particular value scheme does not. Encoded
// arrays support random access, bulk decompression, slicing, and
// serialization. Selection policy stays separate from the encoding builders
// even though both live in this package.
package btrblocks
