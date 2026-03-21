package btrblocks

import (
	"encoding/binary"
	"fmt"
	"io"
	"strings"
)

// patches stores sparse override positions and values for a base encoded array.
type patches[T Integer | Float] struct {
	length  uint64
	offset  uint64
	indices ordinalArray
	values  EncodedArray[T]
}

func newPatches[T Integer | Float](length, offset uint64, indices ordinalArray, values EncodedArray[T]) (*patches[T], error) {
	p := &patches[T]{length: length, offset: offset, indices: indices, values: values}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *patches[T]) BinarySize() uint64 {
	if p == nil {
		return 0
	}
	return 8 + p.indices.BinarySize() + p.values.BinarySize()
}

func (p *patches[T]) WriteTo(w io.Writer) (int64, error) {
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

func (p *patches[T]) Validate() error {
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
	limit := p.offset + p.length
	var prev uint64
	for i := uint64(0); i < p.indices.Length(); i++ {
		idx := p.indices.ValueAt(i)
		if idx < p.offset || idx >= limit {
			return fmt.Errorf("codec: patch index = %d, want [%d, %d)", idx, p.offset, limit)
		}
		if i > 0 && idx <= prev {
			return fmt.Errorf("codec: patch index = %d, want > %d", idx, prev)
		}
		prev = idx
	}
	return nil
}

func (p *patches[T]) Find(offset uint64) (uint64, bool) {
	if p == nil {
		return 0, false
	}
	offset += p.offset
	lo, hi := uint64(0), p.indices.Length()
	for lo < hi {
		mid := lo + (hi-lo)/2
		if p.indices.ValueAt(mid) < offset {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo < p.indices.Length() && p.indices.ValueAt(lo) == offset {
		return lo, true
	}
	return 0, false
}

func (p *patches[T]) ValueAt(offset uint64) (T, bool) {
	var zero T
	if p == nil {
		return zero, false
	}
	idx, ok := p.Find(offset)
	if !ok {
		return zero, false
	}
	return p.values.ValueAt(idx), true
}

func (p *patches[T]) Apply(dst []T) error {
	if p == nil {
		return nil
	}
	for i := uint64(0); i < p.indices.Length(); i++ {
		dst[int(p.indices.ValueAt(i)-p.offset)] = p.values.ValueAt(i)
	}
	return nil
}

func (p *patches[T]) Slice(start, end uint64) (*patches[T], error) {
	if p == nil {
		return nil, nil
	}
	if err := validateSliceBounds(p.length, start, end); err != nil {
		return nil, err
	}
	absStart := p.offset + start
	absEnd := p.offset + end

	first := uint64(0)
	for first < p.indices.Length() && p.indices.ValueAt(first) < absStart {
		first++
	}
	last := first
	for last < p.indices.Length() && p.indices.ValueAt(last) < absEnd {
		last++
	}
	if first == last {
		return nil, nil
	}

	indices := make([]uint64, last-first)
	for i := range indices {
		indices[i] = p.indices.ValueAt(first + uint64(i))
	}
	values, err := p.values.Slice(first, last)
	if err != nil {
		return nil, err
	}
	return newPatches(end-start, absStart, buildOrdinalSlice(indices[len(indices)-1], indices), values)
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
