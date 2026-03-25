package array

import "io"

// BufReader is a lightweight cursor over a byte buffer for zero-copy parsing.
// Read returns subslices of the original Buf — no allocation, no copy.
// The caller must keep Buf alive for the lifetime of any returned subslice.
type BufReader struct {
	Buf []byte
	Off int
}

// Read returns the next n bytes as a subslice and advances the offset.
func (b *BufReader) Read(n int) ([]byte, error) {
	if b.Off+n > len(b.Buf) {
		return nil, io.ErrUnexpectedEOF
	}
	s := b.Buf[b.Off : b.Off+n]
	b.Off += n
	return s, nil
}

// Remaining returns the number of unread bytes.
func (b *BufReader) Remaining() int { return len(b.Buf) - b.Off }
