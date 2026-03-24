package btrblocks

import (
	"encoding/binary"
	"fmt"
	"io"
	"unsafe"

	"github.com/axiomhq/btrblocks/array"
	"github.com/axiomhq/fsst"
)

// fsstArray stores FSST-compressed string data.
type fsstArray struct {
	length   uint64
	tableRaw []byte       // serialized fsst.Table
	codes    []byte       // concatenated compressed string bytes
	offsets  ordinalArray // length+1 offsets into codes
	lengths  ordinalArray // length original uncompressed string lengths
	table    *fsst.Table
}

func (f *fsstArray) Encoding() CodeType { return CodecTypeFSST }
func (f *fsstArray) Length() uint64     { return f.length }
func (f *fsstArray) PType() PType       { return pTypeForType[string]() }

func (f *fsstArray) BinarySize() uint64 {
	return uint64(headerSize) + 4 + uint64(len(f.tableRaw)) + 4 + uint64(len(f.codes)) + f.offsets.BinarySize() + f.lengths.BinarySize()
}

func (f *fsstArray) ValueAt(offset uint64) string {
	if offset >= f.length {
		panic(errOffsetOutOfRange)
	}
	start := f.offsets.ValueAt(offset)
	end := f.offsets.ValueAt(offset + 1)
	if start == end {
		return ""
	}
	decoded := f.table.DecodeAll(f.codes[start:end])
	return unsafe.String(&decoded[0], len(decoded))
}

func (f *fsstArray) DecompressInto(dst []string) error {
	if err := checkDstLen(dst, f.length); err != nil {
		return err
	}
	offsets, err := decompressOrdinals(f.offsets)
	if err != nil {
		return err
	}
	lengths, err := decompressOrdinals(f.lengths)
	if err != nil {
		return err
	}

	decoded := f.table.DecodeAll(f.codes)

	pos := uint64(0)
	for i := uint64(0); i < f.length; i++ {
		l := lengths[i]
		_ = offsets[i] // keep offsets in scope for validation
		dst[i] = string(decoded[pos : pos+l])
		pos += l
	}
	return nil
}

func (f *fsstArray) Decompress() ([]string, error) {
	dst := make([]string, f.length)
	return dst, f.DecompressInto(dst)
}

func (f *fsstArray) Slice(start, end uint64) (EncodedArray[string], error) {
	return sliceToRawArray(f, start, end)
}

func (f *fsstArray) WriteTo(w io.Writer) (int64, error) {
	bodySize := uint64(4) + uint64(len(f.tableRaw)) + uint64(4) + uint64(len(f.codes))
	n, err := header{
		Version:  versionNumber,
		Kind:     CodecTypeFSST,
		ElemType: pTypeForType[string](),
		Length:   f.length,
		BodySize: bodySize,
	}.WriteTo(w)
	if err != nil {
		return n, err
	}

	var buf4 [4]byte

	// table size + table bytes
	binary.LittleEndian.PutUint32(buf4[:], uint32(len(f.tableRaw)))
	nn, err := w.Write(buf4[:])
	n += int64(nn)
	if err != nil {
		return n, err
	}
	nn, err = w.Write(f.tableRaw)
	n += int64(nn)
	if err != nil {
		return n, err
	}

	// codes size + codes bytes
	binary.LittleEndian.PutUint32(buf4[:], uint32(len(f.codes)))
	nn, err = w.Write(buf4[:])
	n += int64(nn)
	if err != nil {
		return n, err
	}
	nn, err = w.Write(f.codes)
	n += int64(nn)
	if err != nil {
		return n, err
	}

	// offsets child
	nn64, err := f.offsets.WriteTo(w)
	n += nn64
	if err != nil {
		return n, err
	}

	// lengths child
	nn64, err = f.lengths.WriteTo(w)
	n += nn64
	return n, err
}

func readFSSTArray(r io.Reader, h header) (EncodedArray[string], error) {
	// read table
	var buf4 [4]byte
	if _, err := io.ReadFull(r, buf4[:]); err != nil {
		return nil, err
	}
	tableSize := binary.LittleEndian.Uint32(buf4[:])
	tableRaw := make([]byte, tableSize)
	if _, err := io.ReadFull(r, tableRaw); err != nil {
		return nil, err
	}

	// read codes
	if _, err := io.ReadFull(r, buf4[:]); err != nil {
		return nil, err
	}
	codesSize := binary.LittleEndian.Uint32(buf4[:])
	codes := make([]byte, codesSize)
	if _, err := io.ReadFull(r, codes); err != nil {
		return nil, err
	}

	// read offsets child
	offsetsHeader, err := readHeader(r)
	if err != nil {
		return nil, err
	}
	offsets, err := readOrdinalArray(r, offsetsHeader)
	if err != nil {
		return nil, fmt.Errorf("codec: fsst offsets %w", err)
	}

	// read lengths child
	lengthsHeader, err := readHeader(r)
	if err != nil {
		return nil, err
	}
	lengths, err := readOrdinalArray(r, lengthsHeader)
	if err != nil {
		return nil, fmt.Errorf("codec: fsst lengths %w", err)
	}

	if offsets.Length() != h.Length+1 {
		return nil, fmt.Errorf("codec: fsst offsets length = %d, want %d", offsets.Length(), h.Length+1)
	}
	if lengths.Length() != h.Length {
		return nil, fmt.Errorf("codec: fsst lengths length = %d, want %d", lengths.Length(), h.Length)
	}

	table := &fsst.Table{}
	if err := table.UnmarshalBinary(tableRaw); err != nil {
		return nil, fmt.Errorf("codec: fsst table: %w", err)
	}

	return &fsstArray{
		length:   h.Length,
		tableRaw: tableRaw,
		codes:    codes,
		offsets:  offsets,
		lengths:  lengths,
		table:    table,
	}, nil
}

func readAnyFSSTArray[T Integer | Float | String](r io.Reader, h header) (EncodedArray[T], error) {
	var zero T
	switch any(zero).(type) {
	case string:
		c, err := readFSSTArray(r, h)
		if err != nil {
			return nil, err
		}
		return any(c).(EncodedArray[T]), nil
	default:
		return nil, fmt.Errorf("codec: FSST not supported for %v", h.ElemType)
	}
}

func buildFSSTArray(arr array.Array[string], ctx planContext) (EncodedArray[string], error) {
	if ctx.depth <= 0 {
		return nil, errDepthExhausted
	}
	n := arr.Length()
	if n == 0 {
		return nil, errDataEmpty
	}

	// Collect all strings as [][]byte for training
	inputs := make([][]byte, n)
	for i := uint64(0); i < n; i++ {
		s := arr.ValueAt(i)
		inputs[i] = unsafe.Slice(unsafe.StringData(s), len(s))
	}

	// Train FSST table
	table := fsst.Train(inputs)

	// Encode each string and build offsets + lengths
	var allCodes []byte
	offsets := make([]uint64, n+1)
	lengths := make([]uint64, n)

	for i := uint64(0); i < n; i++ {
		encoded := table.Encode(inputs[i])
		offsets[i] = uint64(len(allCodes))
		allCodes = append(allCodes, encoded...)
		lengths[i] = uint64(len(inputs[i]))
	}
	offsets[n] = uint64(len(allCodes))

	// Serialize table
	tableRaw, err := table.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("codec: fsst marshal table: %w", err)
	}

	// Compress offsets and lengths children
	childCtx := ctx.descend()
	offsetsCodec, err := buildCompressedOrdinals(offsets, childCtx)
	if err != nil {
		return nil, err
	}
	lengthsCodec, err := buildCompressedOrdinals(lengths, childCtx)
	if err != nil {
		return nil, err
	}

	return &fsstArray{
		length:   n,
		tableRaw: tableRaw,
		codes:    allCodes,
		offsets:  offsetsCodec,
		lengths:  lengthsCodec,
		table:    table,
	}, nil
}

func estimateFSST(stats stringStats, ctx planContext) (float64, bool) {
	if ctx.depth <= 0 {
		return 0, false
	}
	n := stats.Source().Length()
	if n == 0 {
		return 0, false
	}
	return estimateBySample(stats, ctx, buildFSSTArray)
}
