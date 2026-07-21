package array

import (
	"encoding/binary"
	"io"
)

// Virtual presents length computed values as a materializable Array[T]
// without allocating the transformed result. It is the shared leaf view for
// codec transforms (FoR, zigzag, ALP residuals) and computed byte lanes such
// as null masks. valueAt must be pure and total for offsets below length.
type Virtual[T Integer] struct {
	length  uint64
	valueAt func(uint64) T
}

// NewVirtual builds a Virtual array of length rows backed by valueAt.
func NewVirtual[T Integer](length uint64, valueAt func(uint64) T) Virtual[T] {
	return Virtual[T]{length: length, valueAt: valueAt}
}

func (a Virtual[T]) ValueAt(offset uint64) T { return a.valueAt(offset) }
func (a Virtual[T]) Length() uint64          { return a.length }
func (a Virtual[T]) NullCount() uint64       { return 0 }
func (a Virtual[T]) PType() PType            { return PTypeOfPrimitive[T]() }

func (a Virtual[T]) IsValid(offset uint64) bool {
	if offset >= a.length {
		panic("array: virtual index out of range")
	}
	return true
}

func (a Virtual[T]) CopyTo(dst []T) {
	for i := range dst {
		dst[i] = a.valueAt(uint64(i))
	}
}

func (a Virtual[T]) BinarySize() uint64 {
	return uint64(headerSize) + a.length*uint64(PTypeOfPrimitive[T]().ByteWidth())
}

func (a Virtual[T]) Slice(start, end uint64) (Array[T], error) {
	return MaterializePrimitiveSlice(a, start, end)
}

// WriteTo writes a header plus chunked computed values, serializing the
// transformed view without materializing the entire result.
func (a Virtual[T]) WriteTo(w io.Writer) (int64, error) {
	width := uint64(PTypeOfPrimitive[T]().ByteWidth())
	total, err := Header{
		Version:  FormatVersion,
		PType:    PTypeOfPrimitive[T](),
		Length:   a.length,
		NumBytes: a.length * width,
	}.WriteTo(w)
	if err != nil || a.length == 0 {
		return total, err
	}

	// Write in 1024-element chunks to bound memory (8KB at uint64 width)
	// while amortising write syscall overhead.
	chunkElems := min(a.length, 1024)
	buf := make([]byte, chunkElems*width)
	for offset := uint64(0); offset < a.length; {
		chunk := min(a.length-offset, chunkElems)
		putLittleEndian(buf, int(width), int(chunk), func(i int) uint64 {
			return uint64(a.valueAt(offset + uint64(i)))
		})
		n, err := w.Write(buf[:chunk*width])
		total += int64(n)
		if err != nil {
			return total, err
		}
		if uint64(n) != chunk*width {
			return total, io.ErrShortWrite
		}
		offset += chunk
	}
	return total, nil
}

// putLittleEndian encodes count values of the given byte width into buf using
// explicit little-endian byte order. This avoids platform-endianness assumptions
// from unsafe pointer casts.
func putLittleEndian(buf []byte, width, count int, value func(int) uint64) {
	switch width {
	case 1:
		for i := range count {
			buf[i] = byte(value(i))
		}
	case 2:
		for i := range count {
			binary.LittleEndian.PutUint16(buf[i*2:], uint16(value(i)))
		}
	case 4:
		for i := range count {
			binary.LittleEndian.PutUint32(buf[i*4:], uint32(value(i)))
		}
	case 8:
		for i := range count {
			binary.LittleEndian.PutUint64(buf[i*8:], value(i))
		}
	}
}
