package array

import (
	"encoding/binary"
	"fmt"
	"io"
)

// HeaderSize is the fixed size of the binary array header (20 bytes).
const HeaderSize = 20

const (
	// FormatVersion is the current raw-array wire version emitted by writers.
	// Version 1 remains a pre-release draft until the first v1.0.0 tag.
	FormatVersion        = 1
	FlagValidity  uint16 = 1 // Marks an array body prefixed by a validity bitmap.
)

const (
	headerSize             = HeaderSize
	defaultMaxReadLength   = 1 << 26  // 67 million values.
	defaultMaxReadBytes    = 1 << 30  // 1 GiB body.
	defaultMaxDecodedBytes = 64 << 20 // 64 MiB materialized payload.
)

// Header is the fixed 20-byte prefix written before every array body.
// Layout: Version(1) + PType(1) + Flags(2) + Length(8) + BodySize(8), all
// little-endian.
//
// This format intentionally omits magic bytes. Array streams are only entered
// through array/encoded-array read paths that already know they are at an array
// boundary, so version and flags carry the format-evolution contract. See
// FORMAT.md for the versioning and compatibility policy.
type Header struct {
	Version  uint8  // Format version; currently 1.
	PType    PType  // Element type (int8, uint32, string, etc.).
	Flags    uint16 // Array framing flags.
	Length   uint64 // Number of elements in the array.
	NumBytes uint64 // Number of bytes in the body following this header.
}

// readHeaderFromBuf reads a 20-byte header from br (zero-copy).
func readHeaderFromBuf(br *BufReader) (Header, error) {
	buf, err := br.Read(headerSize)
	if err != nil {
		return Header{}, err
	}
	return Header{
		Version:  buf[0],
		PType:    PType(buf[1]),
		Flags:    binary.LittleEndian.Uint16(buf[2:4]),
		Length:   binary.LittleEndian.Uint64(buf[4:12]),
		NumBytes: binary.LittleEndian.Uint64(buf[12:20]),
	}, nil
}

// DefaultMaxWork is the validation budget a decode gets when
// ReadOptions.MaxWork is zero, counted in decoded bytes.
const DefaultMaxWork = 1 << 28

// ReadOptions constrains resource usage when decoding. Its zero value applies
// conservative defaults suitable for untrusted data.
type ReadOptions struct {
	// MaxLength is the maximum number of elements allowed in a single array.
	// Zero uses the default limit of 1<<26. Use math.MaxUint64 explicitly for
	// trusted data that must not have a practical length limit.
	MaxLength uint64

	// MaxBytes is the maximum body size in bytes allowed after a header.
	// Zero uses the default limit of 1<<30. Use math.MaxUint64 explicitly for
	// trusted data that must not have a practical byte limit.
	MaxBytes uint64

	// MaxDecodedBytes is the maximum a single materialization may hold live,
	// counting the buffers its children decode alongside it. Zero uses the
	// default limit of 64 MiB. Use math.MaxUint64 explicitly only when a
	// higher-level memory budget governs trusted data.
	MaxDecodedBytes uint64

	// MaxDepth is the maximum codec-tree nesting depth when decoding encoded
	// arrays. Zero means the decoder's default limit. The codec layer counts
	// levels against it per branch; a negative value admits nothing.
	MaxDepth int

	// MaxWork is the total validation work one codec-tree decode may perform,
	// counted in bytes materialized across every validation scan:
	// MaxDecodedBytes bounds one scan, MaxWork bounds their sum. Zero uses
	// DefaultMaxWork. Like MaxDepth it is charged by the codec layer, which
	// decodes children to validate a node; this package's own readers have no
	// children to scan and ignore it. It is deliberately not derived from the
	// size of the caller's buffer: the same bytes and the same options must
	// always produce the same accept/reject decision.
	MaxWork uint64
}

// WorkLimit returns the effective validation budget in decoded bytes.
func (o ReadOptions) WorkLimit() uint64 {
	if o.MaxWork == 0 {
		return DefaultMaxWork
	}
	return o.MaxWork
}

// DecodedByteLimit returns the effective variable-width decode budget.
func (o ReadOptions) DecodedByteLimit() uint64 {
	if o.MaxDecodedBytes == 0 {
		return defaultMaxDecodedBytes
	}
	return o.MaxDecodedBytes
}

func validateHeader(h Header, opts ReadOptions) error {
	if h.Version != FormatVersion {
		return fmt.Errorf("array: unsupported version = %d", h.Version)
	}
	if h.Flags&^FlagValidity != 0 {
		return fmt.Errorf("array: unsupported flags = 0x%x", h.Flags)
	}
	maxLength := opts.MaxLength
	if maxLength == 0 {
		maxLength = defaultMaxReadLength
	}
	if h.Length > maxLength {
		return fmt.Errorf("array: length %d exceeds limit %d", h.Length, maxLength)
	}
	maxBytes := opts.MaxBytes
	if maxBytes == 0 {
		maxBytes = defaultMaxReadBytes
	}
	if h.NumBytes > maxBytes {
		return fmt.Errorf("array: body size %d exceeds limit %d", h.NumBytes, maxBytes)
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
