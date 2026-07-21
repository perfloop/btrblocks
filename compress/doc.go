// Package compress selects and builds BtrBlocks codec trees for columnar
// arrays. It owns sampling, statistics, estimates, exclusions, and recursive
// selection policy; physical arrays and codec nodes live in the array and
// codec packages.
package compress
