package btrblocks

import (
	"encoding/binary"
	"io"
)

const headerSize = 24

// Header is the fixed 24-byte prefix for every codec node.
// Layout: Version(1) + Kind(1) + ElemType(1) + ChildCount(1) + Flags(4) +
// Length(8) + BodySize(8), all little-endian.
//
// A serialized codec node is:
//
//	[Header][BodySize bytes of codec-local body][ChildCount child codec nodes]
//
// This format intentionally omits magic bytes. Codec streams are only entered
// through typed decode paths that already know they are positioned at a codec
// boundary, so the extra per-node overhead does not buy a useful recovery path.
type Header struct {
	Version    uint8
	Kind       CodecType
	ElemType   PType
	ChildCount uint8
	Flags      uint32
	Length     uint64
	BodySize   uint64
}

func readHeader(r io.Reader) (Header, error) {
	var buf [headerSize]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return Header{}, err
	}
	return Header{
		Version:    buf[0],
		Kind:       CodecType(buf[1]),
		ElemType:   PType(buf[2]),
		ChildCount: buf[3],
		Flags:      binary.LittleEndian.Uint32(buf[4:8]),
		Length:     binary.LittleEndian.Uint64(buf[8:16]),
		BodySize:   binary.LittleEndian.Uint64(buf[16:24]),
	}, nil
}

func (h Header) WriteTo(w io.Writer) (int64, error) {
	var buf [headerSize]byte
	buf[0] = h.Version
	buf[1] = byte(h.Kind)
	buf[2] = byte(h.ElemType)
	buf[3] = h.ChildCount
	binary.LittleEndian.PutUint32(buf[4:8], h.Flags)
	binary.LittleEndian.PutUint64(buf[8:16], h.Length)
	binary.LittleEndian.PutUint64(buf[16:24], h.BodySize)
	n, err := w.Write(buf[:])
	if err == nil && n != len(buf) {
		err = io.ErrShortWrite
	}
	return int64(n), err
}
