package codec

import "fmt"

// readBudget bounds the total validation work one decode may perform, counted
// in bytes materialized. MaxDecodedBytes caps a single materialization;
// this caps their sum, so a tree cannot ask for many scans that each just fit.
//
// A charge is only honest if it covers everything the work it pays for
// touches, so a scan is charged its child's whole-subtree DecodedBytes rather
// than that child's declared length. The two differ without bound: a dict of
// length 1 over a 33M-element values child decodes 33M elements, and charging
// it one visit made 1426 bytes cost seconds and still be accepted. Bytes also
// keep the charge O(1) per element decoded, which a ValueAt scan would not:
// random access costs O(offset) in the delta and run-end codecs, so charging n
// there hid O(n^2) work — 136 bytes of run-end over delta(sequence) once
// burned 16.65s and was then accepted.
//
// Scans over bytes actually present in the stream (bitmaps, packed buffers) are
// not charged: their iteration count is already bounded by the input size.
// Charged scans are the ones a declared length drives, which a child codec can
// inflate from a handful of bytes.
//
// The zero value has nothing to spend, so a readOptions that newReadOptions did
// not build rejects rather than accounting for nothing.
type readBudget struct {
	remaining uint64
}

// spend charges n decoded bytes against the budget.
func (b *readBudget) spend(n uint64) error {
	if n > b.remaining {
		return fmt.Errorf("%w: %d decoded bytes, %d remaining", ErrWorkLimit, n, b.remaining)
	}
	b.remaining -= n
	return nil
}

// readOptions carries one decode's caller limits together with the work budget
// and nesting depth that every node in its tree shares. It is threaded by
// pointer so a child's spending is visible to its parent and siblings; the
// budget is seeded from ReadOptions alone, never from the caller's buffer, so
// the same bytes and options always produce the same accept/reject decision
// whatever follows them in that buffer.
type readOptions struct {
	ReadOptions
	readBudget
	depth int
}

func newReadOptions(opts ReadOptions) *readOptions {
	if opts.MaxDepth == 0 {
		opts.MaxDepth = defaultMaxReadDepth
	}
	return &readOptions{
		ReadOptions: opts,
		readBudget:  readBudget{remaining: opts.WorkLimit()},
	}
}

// enter bounds input-driven recursion: every codec node consumes one level.
// Depth is per branch — leave gives the level back on the way out — so a wide
// tree is not charged for its width. A rejected level is not taken: the caller
// pairs leave with a successful enter, and this readOptions outlives the
// failure for every other branch that shares it.
func (o *readOptions) enter() error {
	if o.depth+1 > o.MaxDepth {
		return fmt.Errorf("codec: nesting depth exceeds limit %d", o.MaxDepth)
	}
	o.depth++
	return nil
}

func (o *readOptions) leave() { o.depth-- }

// decodeLimit is the per-node decode budget this decode's options impose. Nodes
// keep it so the decodes they perform after Load returns answer to the caller's
// ReadOptions rather than to the package default.
func (o *readOptions) decodeLimit() decodeLimit { return decodeLimit(o.DecodedByteLimit()) }

// scanChild decodes child once, sequentially, into a scratch buffer. It
// charges the work budget the child's whole decode footprint — its subtree,
// not its declared length — before decoding, so an over-budget tree is
// rejected rather than measured. Read-path validation goes through it instead
// of walking the child with ValueAt; see readBudget for why.
func scanChild[T Integer | Float | String](child EncodedArray[T], opts *readOptions) ([]T, error) {
	cost, err := child.DecodedBytes()
	if err != nil {
		return nil, err
	}
	if err := opts.spend(cost); err != nil {
		return nil, err
	}
	return decompress(child, opts.DecodedByteLimit())
}
