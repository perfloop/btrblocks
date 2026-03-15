package array

import (
	"encoding/binary"
	"fmt"
	"io"
)

// headerSize is the fixed size of the binary header (20 bytes). Layout: Version(1) + PType(1) + Flags(2) + Length(8) + BodySize(8).
const headerSize = 20

// maxArrayLength is the maximum number of elements we will allocate when decoding. Prevents panic/OOM on corrupt headers.
const maxArrayLength = 1 << 30

// maxStringBufLen is the maximum byte length for the string data buffer when decoding. Prevents OOM on corrupt input.
const maxStringBufLen = 1 << 30

// Header is the fixed 20-byte prefix written before every array body.
// Layout: Version(1) + PType(1) + Flags(2) + Length(8) + BodySize(8), all
// little-endian.
//
// This format intentionally omits magic bytes. Array streams are only entered
// through array/codec decode paths that already know they are at an array
// boundary, so version and flags carry the format-evolution contract.
type Header struct {
	Version  uint8  // Format version; currently 1.
	PType    PType  // Element type (int8, uint32, string, etc.).
	Flags    uint16 // Reserved for future use.
	Length   uint64 // Number of elements in the array.
	BodySize uint64 // Size in bytes of the body following this header.
}

// readHeader reads a 20-byte header from r.
func readHeader(r io.Reader) (Header, error) {
	var buf [headerSize]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return Header{}, err
	}
	return Header{
		Version:  buf[0],
		PType:    PType(buf[1]),
		Flags:    binary.LittleEndian.Uint16(buf[2:4]),
		Length:   binary.LittleEndian.Uint64(buf[4:12]),
		BodySize: binary.LittleEndian.Uint64(buf[12:20]),
	}, nil
}

func validateHeader(h Header) error {
	if h.Version != 1 {
		return fmt.Errorf("array: unsupported version = %d", h.Version)
	}
	if h.Flags != 0 {
		return fmt.Errorf("array: unsupported flags = 0x%x", h.Flags)
	}
	return nil
}

// WriteTo encodes h in LittleEndian and writes it to w. Returns bytes written and any error.
func (h Header) WriteTo(w io.Writer) (int64, error) {
	var buf [headerSize]byte
	buf[0] = h.Version
	buf[1] = byte(h.PType)
	binary.LittleEndian.PutUint16(buf[2:4], h.Flags)
	binary.LittleEndian.PutUint64(buf[4:12], h.Length)
	binary.LittleEndian.PutUint64(buf[12:20], h.BodySize)
	n, err := w.Write(buf[:])
	if err == nil && n != len(buf) {
		err = io.ErrShortWrite
	}
	return int64(n), err
}
