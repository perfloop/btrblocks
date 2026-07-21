package array

import "fmt"

// DefaultMaxBuildBytes is the default limit on one temporary materialization
// created while building.
const DefaultMaxBuildBytes uint64 = 64 << 20

// BuildOptions bounds temporary memory used while building. MaxBytes limits
// each materialized buffer; the limit is per allocation, not cumulative. Zero
// selects DefaultMaxBuildBytes.
//
// It is the one byte-budget knob on the build path: codec and compress spell it
// with their own names for this same type rather than declaring their own.
type BuildOptions struct {
	MaxBytes uint64
}

// buildByteLimit collapses a variadic BuildOptions tail to the single limit the
// builders honour. Extras are rejected rather than silently dropped: accepting
// them now would make it a breaking change to give them meaning later.
func buildByteLimit(opts []BuildOptions) (uint64, error) {
	if len(opts) > 1 {
		return 0, fmt.Errorf("array: at most one BuildOptions is allowed, got %d", len(opts))
	}
	if len(opts) == 1 && opts[0].MaxBytes != 0 {
		return opts[0].MaxBytes, nil
	}
	return DefaultMaxBuildBytes, nil
}
