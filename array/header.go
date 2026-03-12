package array

import (
	"encoding/binary"
	"io"
)

const headerSize = 20

// Header is the fixed 20-byte header written before every array body.
type Header struct {
	Version  uint8
	PType    PType
	Flags    uint16
	Length   uint64
	BodySize uint64
}

// writeHeader encodes h in LittleEndian and writes it to w. Returns bytes written and any error.
func (h Header) WriteTo(w io.Writer) (int64, error) {
	var buf [headerSize]byte
	buf[0] = h.Version
	buf[1] = byte(h.PType)
	binary.LittleEndian.PutUint16(buf[2:4], h.Flags)
	binary.LittleEndian.PutUint64(buf[4:12], h.Length)
	binary.LittleEndian.PutUint64(buf[12:20], h.BodySize)
	n, err := w.Write(buf[:])
	return int64(n), err
}
