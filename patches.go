package btrblocks

import (
	"encoding/binary"
	"fmt"
	"io"
	"strings"

	"github.com/axiomhq/btrblocks/array"
)

// patches stores sparse override positions and values for a base encoded array.
type patches[V Integer | Float, I UnsignedInteger] struct {
	length  uint64
	offset  uint64
	indices EncodedArray[I]
	values  EncodedArray[V]
}

func newPatches[V Integer | Float, I UnsignedInteger](length, offset uint64, indices EncodedArray[I], values EncodedArray[V]) (*patches[V, I], error) {
	p := &patches[V, I]{length: length, offset: offset, indices: indices, values: values}
	if err := p.Validate(); err != nil {
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

func (p *patches[V, I]) Validate() error {
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
	if p.offset > ^uint64(0)-p.length {
		return fmt.Errorf("codec: patch offset %d overflows length %d", p.offset, p.length)
	}
	// Bounds-check first and last index only. This avoids O(N) per-element
	// ValueAt calls through the codec tree during deserialization of untrusted
	// data. The full sorted invariant is guaranteed by construction during
	// compression; adversarial payloads that violate sortedness may produce
	// wrong results in Find/ValueAt point queries but cannot cause out-of-bounds
	// access in Apply (which scatters without order dependency).
	limit := p.offset + p.length
	n := p.indices.Length()
	first := uint64(p.indices.ValueAt(0))
	if first < p.offset || first >= limit {
		return fmt.Errorf("codec: patch index = %d, want [%d, %d)", first, p.offset, limit)
	}
	if n > 1 {
		last := uint64(p.indices.ValueAt(n - 1))
		if last < p.offset || last >= limit {
			return fmt.Errorf("codec: patch index = %d, want [%d, %d)", last, p.offset, limit)
		}
		if last <= first {
			return fmt.Errorf("codec: patch last index %d must be > first index %d", last, first)
		}
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
	indices, err := Decompress(p.indices)
	if err != nil {
		return err
	}
	values, err := Decompress(p.values)
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
	indices, err := Decompress(p.indices)
	if err != nil {
		return err
	}
	values, err := Decompress(p.values)
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

	// Binary search for the first index >= absStart.
	n := p.indices.Length()
	first := uint64(0)
	{
		lo, hi := uint64(0), n
		for lo < hi {
			mid := lo + (hi-lo)/2
			if uint64(p.indices.ValueAt(mid)) < absStart {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		first = lo
	}
	// Binary search for the first index >= absEnd.
	last := first
	{
		lo, hi := first, n
		for lo < hi {
			mid := lo + (hi-lo)/2
			if uint64(p.indices.ValueAt(mid)) < absEnd {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		last = lo
	}
	if first == last {
		return nil, nil
	}

	indices := make([]I, last-first)
	for i := range indices {
		indices[i] = p.indices.ValueAt(first + uint64(i))
	}
	values, err := p.values.Slice(first, last)
	if err != nil {
		return nil, err
	}
	return newPatches[V, I](end-start, absStart, newRawArray(array.NewPrimitivesUnsafe(indices)), values)
}

func prefixPatchError(err error, prefix string) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.HasPrefix(msg, "codec: ") {
		return fmt.Errorf("codec: %s %s", prefix, strings.TrimPrefix(msg, "codec: "))
	}
	return fmt.Errorf("codec: %s %v", prefix, err)
}
