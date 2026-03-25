package array

import (
	"encoding/binary"
	"fmt"
	"io"
)

// HeaderSize is the fixed size of the binary array header (20 bytes).
const HeaderSize = 20

const headerSize = HeaderSize

// Header is the fixed 20-byte prefix written before every array body.
// Layout: Version(1) + PType(1) + Flags(2) + Length(8) + BodySize(8), all
// little-endian.
//
// This format intentionally omits magic bytes. Array streams are only entered
// through array/encoded-array read paths that already know they are at an array
// boundary, so version and flags carry the format-evolution contract.
type Header struct {
	Version uint8  // Format version; currently 1.
	PType   PType  // Element type (int8, uint32, string, etc.).
	Flags   uint16 // Reserved for future use.
	Length  uint64 // Number of elements in the array.
	NumBytes  uint64 // Number of bytes in the body following this header.
}

// readHeader reads a 20-byte header from r.
func readHeader(r io.Reader) (Header, error) {
	var buf [headerSize]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return Header{}, err
	}
	return Header{
		Version: buf[0],
		PType:   PType(buf[1]),
		Flags:   binary.LittleEndian.Uint16(buf[2:4]),
		Length:  binary.LittleEndian.Uint64(buf[4:12]),
		NumBytes:  binary.LittleEndian.Uint64(buf[12:20]),
	}, nil
}

// readHeaderFromBuf reads a 20-byte header from br (zero-copy).
func readHeaderFromBuf(br *BufReader) (Header, error) {
	buf, err := br.Read(headerSize)
	if err != nil {
		return Header{}, err
	}
	return Header{
		Version: buf[0],
		PType:   PType(buf[1]),
		Flags:   binary.LittleEndian.Uint16(buf[2:4]),
		Length:  binary.LittleEndian.Uint64(buf[4:12]),
		NumBytes:  binary.LittleEndian.Uint64(buf[12:20]),
	}, nil
}

// ReadOptions constrains resource usage when decoding from untrusted streams.
// The zero value applies no limits (suitable for trusted internal streams).
type ReadOptions struct {
	// MaxLength is the maximum number of elements allowed in a single array.
	// Zero means no limit.
	MaxLength uint64

	// MaxBytes is the maximum body size in bytes allowed after a header.
	// Zero means no limit.
	MaxBytes uint64
}

func validateHeader(h Header, opts ReadOptions) error {
	if h.Version != 1 {
		return fmt.Errorf("array: unsupported version = %d", h.Version)
	}
	if h.Flags != 0 {
		return fmt.Errorf("array: unsupported flags = 0x%x", h.Flags)
	}
	if opts.MaxLength > 0 && h.Length > opts.MaxLength {
		return fmt.Errorf("array: length %d exceeds limit %d", h.Length, opts.MaxLength)
	}
	if opts.MaxBytes > 0 && h.NumBytes > opts.MaxBytes {
		return fmt.Errorf("array: body size %d exceeds limit %d", h.NumBytes, opts.MaxBytes)
	}
	return nil
}

func platformSliceLimit() uint64 {
	return uint64(^uint(0) >> 1)
}

// WriteTo encodes h in LittleEndian and writes it to w. Returns bytes written and any error.
func (h Header) WriteTo(w io.Writer) (int64, error) {
	var buf [headerSize]byte
	buf[0] = h.Version
	buf[1] = byte(h.PType)
	binary.LittleEndian.PutUint16(buf[2:4], h.Flags)
	binary.LittleEndian.PutUint64(buf[4:12], h.Length)
	binary.LittleEndian.PutUint64(buf[12:20], h.NumBytes)
	n, err := w.Write(buf[:])
	if err == nil && n != len(buf) {
		err = io.ErrShortWrite
	}
	return int64(n), err
}
