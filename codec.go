package btrblocks

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
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
	flagALPHasPatches        uint32 = 1 << 0
	maxDecompressLength             = 1 << 30
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

type plannerExcludes struct {
	integers kindSet
	floats   kindSet
	strings  kindSet
}

type planContext struct {
	depth    int
	isSample bool
	excludes plannerExcludes
}

func newPlanContext(opts Options) planContext {
	opts = normalizeOptions(opts)
	return planContext{depth: opts.MaxDepth}
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

type Codec[T Integer | Float | String] interface {
	io.WriterTo

	Kind() CodeType
	ValueAt(offset uint64) T
	Decode(dst []T) error
	BinarySize() uint64
	Length() uint64
	PType() PType
}

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

func validateDecodeLength(length uint64, dstLen int) error {
	if uint64(dstLen) != length {
		return fmt.Errorf("codec: decode destination length = %d, want %d", dstLen, length)
	}
	return nil
}

func validateHeaderForType[T Integer | Float | String](h header) error {
	if h.Version != versionNumber {
		return fmt.Errorf("codec: unsupported version = %d", h.Version)
	}
	if h.Reserved != 0 {
		return fmt.Errorf("codec: reserved byte = %d, want 0", h.Reserved)
	}
	switch h.Kind {
	case CodecTypeConst, CodecTypeRaw, CodecTypeDict, CodecTypeRunEnd, CodecTypeZigZag, CodecTypeBitpack, CodecTypeFor, CodecTypeSequence, CodecTypeALP:
	default:
		return fmt.Errorf("codec: unknown kind = %d", h.Kind)
	}
	if h.Kind == CodecTypeALP {
		if h.Flags&^flagALPHasPatches != 0 {
			return fmt.Errorf("codec: unsupported ALP flags = 0x%x", h.Flags)
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

func readCodec[T Integer | Float | String](r io.Reader) (Codec[T], error) {
	h, err := readHeader(r)
	if err != nil {
		return nil, err
	}
	return readCodecWithHeader[T](r, h)
}

func readCodecWithHeader[T Integer | Float | String](r io.Reader, h header) (Codec[T], error) {
	if err := validateHeaderForType[T](h); err != nil {
		return nil, err
	}
	switch h.Kind {
	case CodecTypeConst:
		return readConstCodec[T](r, h)
	case CodecTypeRaw:
		return readRawCodec[T](r, h)
	case CodecTypeDict:
		return readDictCodec[T](r, h)
	case CodecTypeRunEnd:
		return readRunEndCodec[T](r, h)
	case CodecTypeZigZag:
		return readAnyZigZagCodec[T](r, h)
	case CodecTypeBitpack:
		return readAnyBitpackCodec[T](r, h)
	case CodecTypeFor:
		return readAnyFoRCodec[T](r, h)
	case CodecTypeSequence:
		return readAnySequenceCodec[T](r, h)
	case CodecTypeALP:
		return readAnyALPCodec[T](r, h)
	default:
		return nil, fmt.Errorf("codec: unknown kind = %d", h.Kind)
	}
}
