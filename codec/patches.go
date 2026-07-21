package codec

import (
	"encoding/binary"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/axiomhq/btrblocks/array"
)

// patches stores sparse override positions and values for a base encoded array.
type patches[V Integer | Float, I UnsignedInteger] struct {
	decodeLimit
	length  uint64
	offset  uint64
	indices EncodedArray[I]
	values  EncodedArray[V]
}

// newPatches assembles a patch set the builder produced. decoded is the index
// child's values, which the builder already holds; validating them costs it no
// extra pass.
func newPatches[V Integer | Float, I UnsignedInteger](length, offset uint64, indices EncodedArray[I], values EncodedArray[V], decoded []I) (*patches[V, I], error) {
	p := &patches[V, I]{length: length, offset: offset, indices: indices, values: values}
	if err := p.validate(); err != nil {
		return nil, err
	}
	if err := p.validateIndices(decoded); err != nil {
		return nil, err
	}
	return p, nil
}

// readPatches assembles a patch set from children just read off the wire. The
// index scan runs over one sequential decode charged to the work budget, and
// only after the O(1) checks have bounded how large that decode can be.
func readPatches[V Integer | Float, I UnsignedInteger](length, offset uint64, indices EncodedArray[I], values EncodedArray[V], opts *readOptions) (*patches[V, I], error) {
	p := &patches[V, I]{decodeLimit: opts.decodeLimit(), length: length, offset: offset, indices: indices, values: values}
	if err := p.validate(); err != nil {
		return nil, err
	}
	decoded, err := scanChild(indices, opts)
	if err != nil {
		return nil, err
	}
	if err := p.validateIndices(decoded); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *patches[V, I]) BinarySize() uint64 {
	if p == nil {
		return 0
	}
	return 8 + p.indices.BinarySize() + p.values.BinarySize()
}

// decodedBytes reports what Apply and Iterate materialize: both children in
// full, alongside the destination their caller already holds. A nil patch set
// costs nothing.
func (p *patches[V, I]) decodedBytes() (uint64, error) {
	if p == nil {
		return 0, nil
	}
	var f decodeFootprint
	f.add(p.indices.DecodedBytes())
	f.add(p.values.DecodedBytes())
	return f.result()
}

func (p *patches[V, I]) WriteTo(w io.Writer) (int64, error) {
	if p == nil {
		return 0, nil
	}
	var offsetBuf [8]byte
	binary.LittleEndian.PutUint64(offsetBuf[:], p.offset)
	nn, err := w.Write(offsetBuf[:])
	if err != nil {
		return int64(nn), err
	}
	if nn != len(offsetBuf) {
		return int64(nn), io.ErrShortWrite
	}
	n, err := p.indices.WriteTo(w)
	if err != nil {
		return int64(nn) + n, err
	}
	nn64, err := p.values.WriteTo(w)
	return int64(nn) + n + nn64, err
}

// validate checks the invariants that cost O(1).
func (p *patches[V, I]) validate() error {
	if p == nil {
		return nil
	}
	if p.indices == nil || p.values == nil {
		return fmt.Errorf("codec: patches missing indices or values")
	}
	if p.indices.Length() != p.values.Length() {
		return fmt.Errorf("codec: patch length mismatch %d vs %d", p.indices.Length(), p.values.Length())
	}
	if p.indices.Length() == 0 {
		return fmt.Errorf("codec: patches length = 0")
	}
	if p.indices.Length() > p.length {
		return fmt.Errorf("codec: patch count %d exceeds logical length %d", p.indices.Length(), p.length)
	}
	if p.offset > ^uint64(0)-p.length {
		return fmt.Errorf("codec: patch offset %d overflows length %d", p.offset, p.length)
	}
	return nil
}

// validateIndices checks that decoded — the index child's values, decoded in
// one sequential pass — is strictly increasing within [offset, offset+length),
// which is what Find and Apply rely on.
func (p *patches[V, I]) validateIndices(decoded []I) error {
	if p == nil {
		return nil
	}
	if uint64(len(decoded)) != p.indices.Length() {
		return fmt.Errorf("codec: patch index scan has %d values, want %d", len(decoded), p.indices.Length())
	}
	limit := p.offset + p.length
	prev := uint64(0)
	for i, rawIdx := range decoded {
		idx := uint64(rawIdx)
		if idx < p.offset || idx >= limit {
			return fmt.Errorf("codec: patch index = %d, want [%d, %d)", idx, p.offset, limit)
		}
		if i > 0 && idx <= prev {
			return fmt.Errorf("codec: patch indices not strictly increasing at position %d: %d <= %d", i, idx, prev)
		}
		prev = idx
	}
	return nil
}

func (p *patches[V, I]) Find(offset uint64) (uint64, bool) {
	if p == nil {
		return 0, false
	}
	offset += p.offset
	lo, hi := uint64(0), p.indices.Length()
	for lo < hi {
		mid := lo + (hi-lo)/2
		if uint64(p.indices.ValueAt(mid)) < offset {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo < p.indices.Length() && uint64(p.indices.ValueAt(lo)) == offset {
		return lo, true
	}
	return 0, false
}

func (p *patches[V, I]) ValueAt(offset uint64) (V, bool) {
	var zero V
	if p == nil {
		return zero, false
	}
	idx, ok := p.Find(offset)
	if !ok {
		return zero, false
	}
	return p.values.ValueAt(idx), true
}

// Iterate bulk-decompresses both children and calls fn for each patch with the
// offset-adjusted position and value. Callers that need custom combine logic
// (e.g. ALPRD, which patches the left part rather than the final value) use this
// instead of Apply.
func (p *patches[V, I]) Iterate(fn func(position uint64, value V)) error {
	if p == nil {
		return nil
	}
	indices, err := decompress(p.indices, p.maxDecodedBytes())
	if err != nil {
		return err
	}
	values, err := decompress(p.values, p.maxDecodedBytes())
	if err != nil {
		return err
	}
	for i, idx := range indices {
		fn(uint64(idx)-p.offset, values[i])
	}
	return nil
}

func (p *patches[V, I]) Apply(dst []V) error {
	if p == nil {
		return nil
	}
	indices, err := decompress(p.indices, p.maxDecodedBytes())
	if err != nil {
		return err
	}
	values, err := decompress(p.values, p.maxDecodedBytes())
	if err != nil {
		return err
	}
	for i, idx := range indices {
		dst[int(uint64(idx)-p.offset)] = values[i]
	}
	return nil
}

func (p *patches[V, I]) Slice(start, end uint64) (*patches[V, I], error) {
	if p == nil {
		return nil, nil
	}
	if err := array.ValidateSliceBounds(p.length, start, end); err != nil {
		return nil, err
	}
	absStart := p.offset + start
	absEnd := p.offset + end

	// Decode the index child once instead of searching and copying through its
	// ValueAt: that costs O(offset) per read in the delta codec, which makes
	// slicing k patches O(k^2) on a tree the loader accepted. validateIndices
	// already established that the decoded indices are strictly increasing.
	decoded, err := decompress(p.indices, p.maxDecodedBytes())
	if err != nil {
		return nil, fmt.Errorf("codec: slice [%d, %d) decodes all %d patch indices: %w", start, end, p.indices.Length(), err)
	}
	first := sort.Search(len(decoded), func(i int) bool { return uint64(decoded[i]) >= absStart })
	last := first + sort.Search(len(decoded)-first, func(i int) bool { return uint64(decoded[first+i]) >= absEnd })
	if first == last {
		return nil, nil
	}

	indices := make([]I, last-first)
	copy(indices, decoded[first:last])
	values, err := p.values.Slice(uint64(first), uint64(last))
	if err != nil {
		return nil, err
	}
	sliced, err := newPatches(end-start, absStart, newRawArray(array.NewPrimitivesUnsafe(indices)), values, indices)
	if err != nil {
		return nil, err
	}
	// A slice of a loaded patch set decodes under the same budget its source did.
	sliced.decodeLimit = p.decodeLimit
	return sliced, nil
}

func prefixPatchError(err error, prefix string) error {
	if err == nil {
		return nil
	}
	return prefixedPatchError{prefix: prefix, err: err}
}

type prefixedPatchError struct {
	prefix string
	err    error
}

func (e prefixedPatchError) Error() string {
	return "codec: " + e.prefix + " " + strings.TrimPrefix(e.err.Error(), "codec: ")
}

func (e prefixedPatchError) Unwrap() error { return e.err }
