package btrblocks

type plannerExcludes struct {
	integers excludeSet
	floats   excludeSet
	strings  excludeSet
}

// Options controls codec-tree selection. Its zero value uses a maximum depth
// of three and permits every codec family.
type Options struct {
	MaxDepth int
	excludes plannerExcludes
}

// WithMaxDepth returns a copy of o with the recursive codec depth set to d.
// Non-positive values select the default depth of three.
func (o Options) WithMaxDepth(d int) Options {
	o.MaxDepth = d
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
	return opts
}
