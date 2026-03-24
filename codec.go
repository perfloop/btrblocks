package btrblocks

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/axiomhq/btrblocks/array"
)

var (
	errOffsetOutOfRange = errors.New("offset out of range")
	errDepthExhausted   = errors.New("depth exhausted")
	errDataEmpty        = errors.New("data is empty")
)

const (
	versionNumber                   = 1
	headerSize                      = 24
	primitiveArrayHeaderSize        = 20
	flagBitpackHasPatches    uint32 = 1 << 0
	flagALPHasPatches        uint32 = 1 << 0
)

type kindSet uint16

func (e kindSet) has(kind CodeType) bool {
	return e&(1<<kind) != 0
}

func (e kindSet) with(kinds ...CodeType) kindSet {
	for _, kind := range kinds {
		e |= 1 << kind
	}
	return e
}

// plannerExcludes groups per-domain encoding exclusions for recursive planning.
type plannerExcludes struct {
	integers kindSet
	floats   kindSet
	strings  kindSet
}

// planContext carries planner depth, sampling state, and recursive exclusions.
type planContext struct {
	depth    int
	isSample bool
	excludes plannerExcludes
}

func newPlanContext(opts Options) planContext {
	opts = normalizeOptions(opts)
	return planContext{depth: opts.MaxDepth, excludes: opts.excludes}
}

func (c planContext) descend() planContext {
	if c.depth > 0 {
		c.depth--
	}
	return c
}

func (c planContext) sampled() planContext {
	c.isSample = true
	return c
}

func (c planContext) withIntegerExcludes(kinds ...CodeType) planContext {
	c.excludes.integers = c.excludes.integers.with(kinds...)
	return c
}

func (c planContext) withFloatExcludes(kinds ...CodeType) planContext {
	c.excludes.floats = c.excludes.floats.with(kinds...)
	return c
}

func (c planContext) withStringExcludes(kinds ...CodeType) planContext {
	c.excludes.strings = c.excludes.strings.with(kinds...)
	return c
}

func withTypeExcludes[T Integer | Float | String](ctx planContext, kinds ...CodeType) planContext {
	var zero T
	switch any(zero).(type) {
	case string:
		return ctx.withStringExcludes(kinds...)
	case float32, float64:
		return ctx.withFloatExcludes(kinds...)
	default:
		return ctx.withIntegerExcludes(kinds...)
	}
}

func (c planContext) excludesInteger(kind CodeType) bool {
	return c.excludes.integers.has(kind)
}

func (c planContext) excludesFloat(kind CodeType) bool {
	return c.excludes.floats.has(kind)
}

func (c planContext) excludesString(kind CodeType) bool {
	return c.excludes.strings.has(kind)
}

// EncodedArray is a typed encoded leaf node in the primitive/string compression tree.
type EncodedArray[T Integer | Float | String] interface {
	io.WriterTo
	Encoding() CodeType
	ValueAt(offset uint64) T
	Slice(start, end uint64) (EncodedArray[T], error)
	BinarySize() uint64
	Length() uint64
	PType() PType
	// Decompress recursively decodes the entire tree into a flat slice.
	Decompress() ([]T, error)
}

// header is the fixed encoded-array stream prefix written before each node body.
type header struct {
	Version  uint8
	Kind     CodeType
	ElemType PType
	Reserved uint8
	Flags    uint32
	Length   uint64
	BodySize uint64
}

func readHeader(r io.Reader) (header, error) {
	var buf [headerSize]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return header{}, err
	}
	return header{
		Version:  buf[0],
		Kind:     CodeType(buf[1]),
		ElemType: PType(buf[2]),
		Reserved: buf[3],
		Flags:    binary.LittleEndian.Uint32(buf[4:8]),
		Length:   binary.LittleEndian.Uint64(buf[8:16]),
		BodySize: binary.LittleEndian.Uint64(buf[16:24]),
	}, nil
}

func (h header) WriteTo(w io.Writer) (int64, error) {
	var buf [headerSize]byte
	buf[0] = h.Version
	buf[1] = byte(h.Kind)
	buf[2] = byte(h.ElemType)
	buf[3] = h.Reserved
	binary.LittleEndian.PutUint32(buf[4:8], h.Flags)
	binary.LittleEndian.PutUint64(buf[8:16], h.Length)
	binary.LittleEndian.PutUint64(buf[16:24], h.BodySize)
	n, err := w.Write(buf[:])
	if err == nil && n != len(buf) {
		err = io.ErrShortWrite
	}
	return int64(n), err
}

func validateSliceBounds(length, start, end uint64) error {
	if start > end {
		return fmt.Errorf("codec: slice start = %d, want <= %d", start, end)
	}
	if end > length {
		return fmt.Errorf("codec: slice end = %d, want <= %d", end, length)
	}
	return nil
}

func materializeSlice[T Integer | Float | String](src interface {
	Length() uint64
	ValueAt(uint64) T
}, start, end uint64) (array.Array[T], error) {
	if err := validateSliceBounds(src.Length(), start, end); err != nil {
		return nil, err
	}
	values := make([]T, end-start)
	for i := range values {
		values[i] = src.ValueAt(start + uint64(i))
	}
	return buildArray(values), nil
}

func sliceToRawArray[T Integer | Float | String](src interface {
	Length() uint64
	ValueAt(uint64) T
}, start, end uint64) (EncodedArray[T], error) {
	values, err := materializeSlice(src, start, end)
	if err != nil {
		return nil, err
	}
	return newRawArray(values), nil
}

func validateHeaderForType[T Integer | Float | String](h header) error {
	if h.Version != versionNumber {
		return fmt.Errorf("codec: unsupported version = %d", h.Version)
	}
	if h.Reserved != 0 {
		return fmt.Errorf("codec: reserved byte = %d, want 0", h.Reserved)
	}
	switch h.Kind {
	case CodecTypeConst, CodecTypeRaw, CodecTypeDict, CodecTypeRunEnd, CodecTypeZigZag, CodecTypeBitpack, CodecTypeFor, CodecTypeSequence, CodecTypeALP, CodecTypeFSST, CodecTypeALPRD:
	default:
		return fmt.Errorf("codec: unknown kind = %d", h.Kind)
	}
	if h.Kind == CodecTypeBitpack {
		if h.Flags&^flagBitpackHasPatches != 0 {
			return fmt.Errorf("codec: unsupported bitpack flags = 0x%x", h.Flags)
		}
	} else if h.Kind == CodecTypeALP {
		if h.Flags&^flagALPHasPatches != 0 {
			return fmt.Errorf("codec: unsupported ALP flags = 0x%x", h.Flags)
		}
	} else if h.Kind == CodecTypeALPRD {
		if h.Flags&^flagALPRDHasPatches != 0 {
			return fmt.Errorf("codec: unsupported ALPRD flags = 0x%x", h.Flags)
		}
	} else if h.Flags != 0 {
		return fmt.Errorf("codec: unsupported flags = 0x%x", h.Flags)
	}

	expected := pTypeForType[T]()
	if h.ElemType != expected {
		return fmt.Errorf("codec: element type = %v, want %v", h.ElemType, expected)
	}
	return nil
}

func readEncodedArray[T Integer | Float | String](r io.Reader) (EncodedArray[T], error) {
	h, err := readHeader(r)
	if err != nil {
		return nil, err
	}
	return readEncodedArrayWithHeader[T](r, h)
}

func readEncodedArrayWithHeader[T Integer | Float | String](r io.Reader, h header) (EncodedArray[T], error) {
	if err := validateHeaderForType[T](h); err != nil {
		return nil, err
	}
	switch h.Kind {
	case CodecTypeConst:
		return readConstArray[T](r, h)
	case CodecTypeRaw:
		return readRawArray[T](r, h)
	case CodecTypeDict:
		return readDictArray[T](r, h)
	case CodecTypeRunEnd:
		return readRunEndArray[T](r, h)
	case CodecTypeZigZag:
		return readAnyZigZagArray[T](r, h)
	case CodecTypeBitpack:
		return readAnyBitPackedArray[T](r, h)
	case CodecTypeFor:
		return readAnyFoRArray[T](r, h)
	case CodecTypeSequence:
		return readAnySequenceArray[T](r, h)
	case CodecTypeALP:
		return readAnyALPArray[T](r, h)
	case CodecTypeALPRD:
		return readAnyALPRDArray[T](r, h)
	case CodecTypeFSST:
		return readAnyFSSTArray[T](r, h)
	default:
		return nil, fmt.Errorf("codec: unknown kind = %d", h.Kind)
	}
}
