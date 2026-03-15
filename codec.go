package btrblocks

import (
	"errors"
	"fmt"
	"io"
)

var (
	errOffsetOutOfRange = errors.New("offset out of range")
	errDepthExhausted   = errors.New("depth exhausted")
	errDataEmpty        = errors.New("data is empty")
)

type CodecType uint8

const (
	CodecTypeUnknown CodecType = iota
	CodecTypeConst
	CodecTypeRaw
	CodecTypeDict
	CodecTypeRunend
	CodecTypeZigzag
	CodecTypeBitpacking
)

// Scheme is a untyped structural interface
type Scheme interface {
	Children() []Scheme
}

// Codec is a typed access interface
type Codec[T Integer | Float | String] interface {
	Scheme
	io.WriterTo
	ValueAt(offset uint64) (T, error)
	// Decode materializes all values into dst. len(dst) must equal Length().
	// Significantly faster than per-element ValueAt for nested codecs (Dict,
	// Runend, Zigzag) because it bulk-decodes children once and avoids repeated
	// tree traversal.
	Decode(dst []T) error
	BinarySize() uint64
	Length() uint64
	PType() PType
}

func readCodec[T Integer | Float | String](r io.Reader) (Codec[T], error) {
	header, err := readHeader(r)
	if err != nil {
		return nil, err
	}
	return readCodecWithHeader[T](r, header)
}

func readCodecWithHeader[T Integer | Float | String](r io.Reader, header Header) (Codec[T], error) {
	if err := validateCodecHeader[T](header); err != nil {
		return nil, err
	}
	switch header.Kind {
	case CodecTypeConst:
		return readConstCodec[T](r, header)
	case CodecTypeRaw:
		return readRawCodec[T](r, header)
	case CodecTypeDict:
		return readDictCodec[T](r, header)
	case CodecTypeRunend:
		return readRunendCodec[T](r, header)
	case CodecTypeZigzag:
		return readAnyZigzagCodec[T](r, header)
	case CodecTypeBitpacking:
		return readAnyBitpackingCodec[T](r, header)
	default:
		return nil, fmt.Errorf("codec: unknown codec type = %d", header.Kind)
	}
}

func validateCodecHeader[T Integer | Float | String](header Header) error {
	if header.Version != 1 {
		return fmt.Errorf("codec: unsupported version = %d", header.Version)
	}
	if header.Flags != 0 {
		return fmt.Errorf("codec: unsupported flags = 0x%x", header.Flags)
	}
	expected := pTypeForType[T]()
	if header.ElemType != expected {
		return fmt.Errorf("codec: element type = %v, want %v", header.ElemType, expected)
	}
	return nil
}
