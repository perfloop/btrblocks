package compress

// excludeSet is a bitmask of CodecType values used by the selector.
type excludeSet uint32

func (s excludeSet) Has(kind CodecType) bool { return s&(1<<kind) != 0 }

func (s excludeSet) With(kinds ...CodecType) excludeSet {
	for _, kind := range kinds {
		s |= 1 << kind
	}
	return s
}

type plannerExcludes struct {
	integers excludeSet
	floats   excludeSet
	strings  excludeSet
}

// Options controls codec-tree selection. Its zero value uses a maximum depth
// of three, permits every codec family, and limits each temporary build
// materialization to DefaultMaxBuildBytes.
type Options struct {
	MaxDepth      int
	MaxBuildBytes uint64
	excludes      plannerExcludes
}

// WithMaxDepth returns a copy of o with the recursive codec depth set to d.
// Non-positive values select the default depth of three.
func (o Options) WithMaxDepth(d int) Options {
	o.MaxDepth = d
	return o
}

// WithMaxBuildBytes returns a copy of o with the per-materialization build
// limit set to n. Zero selects DefaultMaxBuildBytes.
func (o Options) WithMaxBuildBytes(n uint64) Options {
	o.MaxBuildBytes = n
	return o
}

// WithExcludeInteger returns a copy of o that excludes kinds from integer
// selection, including recursive integer children.
func (o Options) WithExcludeInteger(kinds ...CodecType) Options {
	o.excludes.integers = o.excludes.integers.With(kinds...)
	return o
}

// WithExcludeFloat returns a copy of o that excludes kinds from float
// selection, including recursive float children.
func (o Options) WithExcludeFloat(kinds ...CodecType) Options {
	o.excludes.floats = o.excludes.floats.With(kinds...)
	return o
}

// WithExcludeString returns a copy of o that excludes kinds from string
// selection, including recursive string children.
func (o Options) WithExcludeString(kinds ...CodecType) Options {
	o.excludes.strings = o.excludes.strings.With(kinds...)
	return o
}

const defaultMaxDepth = 3

func normalizeOptions(opts Options) Options {
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = defaultMaxDepth
	}
	if opts.MaxBuildBytes == 0 {
		opts.MaxBuildBytes = DefaultMaxBuildBytes
	}
	return opts
}
