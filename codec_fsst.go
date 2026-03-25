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
type fsstArray[I, J UnsignedInteger] struct {
	length   uint64
	tableRaw []byte       // serialized fsst.Table
	codes    []byte       // concatenated compressed string bytes
	offsets  EncodedArray[I] // length+1 offsets into codes
	lengths  EncodedArray[J] // length original uncompressed string lengths
	table    *fsst.Table
}

func (f *fsstArray[I, J]) Encoding() CodeType { return CodecTypeFSST }
func (f *fsstArray[I, J]) Length() uint64      { return f.length }
func (f *fsstArray[I, J]) PType() PType        { return array.PTypeForType[string]() }

func (f *fsstArray[I, J]) BinarySize() uint64 {
	return uint64(headerSize) + 4 + uint64(len(f.tableRaw)) + 4 + uint64(len(f.codes)) + f.offsets.BinarySize() + f.lengths.BinarySize()
}

func (f *fsstArray[I, J]) ValueAt(offset uint64) string {
	if offset >= f.length {
		panic(errOffsetOutOfRange)
	}
	start := uint64(f.offsets.ValueAt(offset))
	end := uint64(f.offsets.ValueAt(offset + 1))
	if start == end {
		return ""
	}
	decoded := f.table.DecodeAll(f.codes[start:end])
	return unsafe.String(&decoded[0], len(decoded))
}

func (f *fsstArray[I, J]) DecompressInto(dst []string) error {
	if err := checkDstLen(dst, f.length); err != nil {
		return err
	}
	lengths, err := Decompress(f.lengths)
	if err != nil {
		return err
	}

	decoded := f.table.DecodeAll(f.codes)

	pos := uint64(0)
	for i := uint64(0); i < f.length; i++ {
		l := uint64(lengths[i])
		if l == 0 {
			dst[i] = ""
		} else {
			buf := decoded[pos : pos+l]
			dst[i] = unsafe.String(unsafe.SliceData(buf), len(buf))
		}
		pos += l
	}
	return nil
}


func (f *fsstArray[I, J]) Slice(start, end uint64) (EncodedArray[string], error) {
	return sliceToRawArray(f, start, end)
}

func (f *fsstArray[I, J]) WriteTo(w io.Writer) (int64, error) {
	bodySize := uint64(4) + uint64(len(f.tableRaw)) + uint64(4) + uint64(len(f.codes))
	n, err := header{
		Version:  versionNumber,
		Kind:     CodecTypeFSST,
		ElemType: array.PTypeForType[string](),
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

// readFSSTWithLengths reads the lengths child and constructs fsstArray[I, J].
func readFSSTWithLengths[I, J UnsignedInteger](h header, tableRaw, codes []byte, table *fsst.Table, offsets EncodedArray[I], r io.Reader, opts ReadOptions) (EncodedArray[string], error) {
	lengthsHeader, err := readHeader(r)
	if err != nil {
		return nil, err
	}
	lengths, err := readEncodedArrayWithHeader[J](r, lengthsHeader, opts)
	if err != nil {
		return nil, fmt.Errorf("codec: fsst lengths %w", err)
	}
	if lengths.Length() != h.Length {
		return nil, fmt.Errorf("codec: fsst lengths length = %d, want %d", lengths.Length(), h.Length)
	}
	return &fsstArray[I, J]{
		length:   h.Length,
		tableRaw: tableRaw,
		codes:    codes,
		offsets:  offsets,
		lengths:  lengths,
		table:    table,
	}, nil
}

// readFSSTWithOffsets reads offsets, then dispatches on lengths ElemType.
func readFSSTWithOffsets[I UnsignedInteger](h header, tableRaw, codes []byte, table *fsst.Table, offsetsHeader header, r io.Reader, opts ReadOptions) (EncodedArray[string], error) {
	offsets, err := readEncodedArrayWithHeader[I](r, offsetsHeader, opts)
	if err != nil {
		return nil, fmt.Errorf("codec: fsst offsets %w", err)
	}
	if offsets.Length() != h.Length+1 {
		return nil, fmt.Errorf("codec: fsst offsets length = %d, want %d", offsets.Length(), h.Length+1)
	}
	lengthsHeader, err := readHeader(r)
	if err != nil {
		return nil, err
	}
	switch lengthsHeader.ElemType {
	case PTypeUint8:
		return readFSSTWithLengthsTyped[I, uint8](h, tableRaw, codes, table, offsets, r, lengthsHeader, opts)
	case PTypeUint16:
		return readFSSTWithLengthsTyped[I, uint16](h, tableRaw, codes, table, offsets, r, lengthsHeader, opts)
	case PTypeUint32:
		return readFSSTWithLengthsTyped[I, uint32](h, tableRaw, codes, table, offsets, r, lengthsHeader, opts)
	case PTypeUint64:
		return readFSSTWithLengthsTyped[I, uint64](h, tableRaw, codes, table, offsets, r, lengthsHeader, opts)
	default:
		return nil, fmt.Errorf("codec: fsst lengths child type = %v, want unsigned integer", lengthsHeader.ElemType)
	}
}

func readFSSTWithLengthsTyped[I, J UnsignedInteger](h header, tableRaw, codes []byte, table *fsst.Table, offsets EncodedArray[I], r io.Reader, lengthsHeader header, opts ReadOptions) (EncodedArray[string], error) {
	lengths, err := readEncodedArrayWithHeader[J](r, lengthsHeader, opts)
	if err != nil {
		return nil, fmt.Errorf("codec: fsst lengths %w", err)
	}
	if lengths.Length() != h.Length {
		return nil, fmt.Errorf("codec: fsst lengths length = %d, want %d", lengths.Length(), h.Length)
	}
	return &fsstArray[I, J]{
		length:   h.Length,
		tableRaw: tableRaw,
		codes:    codes,
		offsets:  offsets,
		lengths:  lengths,
		table:    table,
	}, nil
}

func readFSSTArray(r io.Reader, h header, opts ReadOptions) (EncodedArray[string], error) {
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

	if expectedBody := uint64(4) + uint64(tableSize) + uint64(4) + uint64(codesSize); h.BodySize != expectedBody {
		return nil, fmt.Errorf("codec: fsst body size = %d, want %d", h.BodySize, expectedBody)
	}

	table := &fsst.Table{}
	if err := table.UnmarshalBinary(tableRaw); err != nil {
		return nil, fmt.Errorf("codec: fsst table: %w", err)
	}

	// read offsets header
	offsetsHeader, err := readHeader(r)
	if err != nil {
		return nil, err
	}

	switch offsetsHeader.ElemType {
	case PTypeUint8:
		return readFSSTWithOffsets[uint8](h, tableRaw, codes, table, offsetsHeader, r, opts)
	case PTypeUint16:
		return readFSSTWithOffsets[uint16](h, tableRaw, codes, table, offsetsHeader, r, opts)
	case PTypeUint32:
		return readFSSTWithOffsets[uint32](h, tableRaw, codes, table, offsetsHeader, r, opts)
	case PTypeUint64:
		return readFSSTWithOffsets[uint64](h, tableRaw, codes, table, offsetsHeader, r, opts)
	default:
		return nil, fmt.Errorf("codec: fsst offsets child type = %v, want unsigned integer", offsetsHeader.ElemType)
	}
}

func readAnyFSSTArray[T Integer | Float | String](r io.Reader, h header, opts ReadOptions) (EncodedArray[T], error) {
	var zero T
	switch any(zero).(type) {
	case string:
		c, err := readFSSTArray(r, h, opts)
		if err != nil {
			return nil, err
		}
		return any(c).(EncodedArray[T]), nil
	default:
		return nil, fmt.Errorf("codec: FSST not supported for %v", h.ElemType)
	}
}

func buildFSSTArray(arr array.ArrayCore[string], ctx planContext) (EncodedArray[string], error) {
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

	// Use max(maxOffset, maxLength) to pick a single width for both.
	childCtx := ctx.descend()
	var maxOff, maxLen uint64
	for _, v := range offsets {
		if v > maxOff {
			maxOff = v
		}
	}
	for _, v := range lengths {
		if v > maxLen {
			maxLen = v
		}
	}
	maxVal := maxOff
	if maxLen > maxVal {
		maxVal = maxLen
	}

	switch {
	case maxVal <= uint64(^uint8(0)):
		return buildFSSTTyped[uint8](n, tableRaw, allCodes, table, offsets, lengths, childCtx)
	case maxVal <= uint64(^uint16(0)):
		return buildFSSTTyped[uint16](n, tableRaw, allCodes, table, offsets, lengths, childCtx)
	case maxVal <= uint64(^uint32(0)):
		return buildFSSTTyped[uint32](n, tableRaw, allCodes, table, offsets, lengths, childCtx)
	default:
		return buildFSSTTyped[uint64](n, tableRaw, allCodes, table, offsets, lengths, childCtx)
	}
}

func buildFSSTTyped[I UnsignedInteger](n uint64, tableRaw, allCodes []byte, table *fsst.Table, offsets, lengths []uint64, ctx planContext) (EncodedArray[string], error) {
	narrowOff := make([]I, len(offsets))
	for i, v := range offsets {
		narrowOff[i] = I(v)
	}
	offsetsCodec, err := compressArray(array.NewPrimitivesUnsafe(narrowOff), ctx)
	if err != nil {
		return nil, err
	}
	narrowLen := make([]I, len(lengths))
	for i, v := range lengths {
		narrowLen[i] = I(v)
	}
	lengthsCodec, err := compressArray(array.NewPrimitivesUnsafe(narrowLen), ctx)
	if err != nil {
		return nil, err
	}
	return &fsstArray[I, I]{
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
