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
	tableRaw []byte          // serialized fsst.Table
	codes    []byte          // concatenated compressed string bytes
	offsets  EncodedArray[I] // length+1 offsets into codes
	lengths  EncodedArray[J] // length original uncompressed string lengths
	table    *fsst.Table
}

func (f *fsstArray[I, J]) Encoding() CodeType { return CodecTypeFSST }
func (f *fsstArray[I, J]) Length() uint64     { return f.length }
func (f *fsstArray[I, J]) PType() PType       { return array.PTypeForType[string]() }

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

// DecompressInto decodes FSST-compressed strings into dst. It uses per-string
// Decode into a single pre-sized output buffer rather than DecodeAll, which
// over-allocates at 4x the compressed size. The output buffer is exactly sized
// from the pre-decoded lengths, and each dst[i] aliases it via unsafe.String.
func (f *fsstArray[I, J]) DecompressInto(dst []string) error {
	if err := checkDstLen(dst, f.length); err != nil {
		return err
	}
	lengths, err := Decompress(f.lengths)
	if err != nil {
		return err
	}
	offsets, err := Decompress(f.offsets)
	if err != nil {
		return err
	}

	totalLen := uint64(0)
	for _, l := range lengths {
		totalLen += uint64(l)
	}
	outBuf := make([]byte, totalLen)

	pos := uint64(0)
	for i := uint64(0); i < f.length; i++ {
		l := uint64(lengths[i])
		if l == 0 {
			dst[i] = ""
		} else {
			codeStart := uint64(offsets[i])
			codeEnd := uint64(offsets[i+1])
			f.table.Decode(outBuf[pos:], f.codes[codeStart:codeEnd])
			buf := outBuf[pos : pos+l]
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
	n, err := codecHeader{
		Version:  versionNumber,
		Kind:     CodecTypeFSST,
		ElemType: array.PTypeForType[string](),
		Length:   f.length,
		NumBytes: bodySize,
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

// readFSSTWithOffsets reads offsets, then dispatches on lengths ElemType.
func readFSSTWithOffsets[I UnsignedInteger](h codecHeader, tableRaw, codes []byte, table *fsst.Table, offsetsHeader codecHeader, br *array.BufReader, opts ReadOptions) (EncodedArray[string], error) {
	offsets, err := readEncodedArrayWithHeader[I](br, offsetsHeader, opts)
	if err != nil {
		return nil, fmt.Errorf("codec: fsst offsets %w", err)
	}
	if offsets.Length() != h.Length+1 {
		return nil, fmt.Errorf("codec: fsst offsets length = %d, want %d", offsets.Length(), h.Length+1)
	}
	lengthsHeader, err := readHeader(br)
	if err != nil {
		return nil, err
	}
	switch lengthsHeader.ElemType {
	case PTypeUint8:
		return readFSSTWithLengthsTyped[I, uint8](h, tableRaw, codes, table, offsets, br, lengthsHeader, opts)
	case PTypeUint16:
		return readFSSTWithLengthsTyped[I, uint16](h, tableRaw, codes, table, offsets, br, lengthsHeader, opts)
	case PTypeUint32:
		return readFSSTWithLengthsTyped[I, uint32](h, tableRaw, codes, table, offsets, br, lengthsHeader, opts)
	case PTypeUint64:
		return readFSSTWithLengthsTyped[I, uint64](h, tableRaw, codes, table, offsets, br, lengthsHeader, opts)
	default:
		return nil, fmt.Errorf("codec: fsst lengths child type = %v, want unsigned integer", lengthsHeader.ElemType)
	}
}

func readFSSTWithLengthsTyped[I, J UnsignedInteger](h codecHeader, tableRaw, codes []byte, table *fsst.Table, offsets EncodedArray[I], br *array.BufReader, lengthsHeader codecHeader, opts ReadOptions) (EncodedArray[string], error) {
	lengths, err := readEncodedArrayWithHeader[J](br, lengthsHeader, opts)
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

func readFSSTArray(br *array.BufReader, h codecHeader, opts ReadOptions) (EncodedArray[string], error) {
	// read table
	data, err := br.Read(4)
	if err != nil {
		return nil, err
	}
	tableSize := binary.LittleEndian.Uint32(data)
	tableRaw, err := br.Read(int(tableSize))
	if err != nil {
		return nil, err
	}

	// read codes
	data, err = br.Read(4)
	if err != nil {
		return nil, err
	}
	codesSize := binary.LittleEndian.Uint32(data)
	codes, err := br.Read(int(codesSize))
	if err != nil {
		return nil, err
	}

	if expectedBody := uint64(4) + uint64(tableSize) + uint64(4) + uint64(codesSize); h.NumBytes != expectedBody {
		return nil, fmt.Errorf("codec: fsst body size = %d, want %d", h.NumBytes, expectedBody)
	}

	table := &fsst.Table{}
	if err := table.UnmarshalBinary(tableRaw); err != nil {
		return nil, fmt.Errorf("codec: fsst table: %w", err)
	}

	// read offsets header
	offsetsHeader, err := readHeader(br)
	if err != nil {
		return nil, err
	}

	switch offsetsHeader.ElemType {
	case PTypeUint8:
		return readFSSTWithOffsets[uint8](h, tableRaw, codes, table, offsetsHeader, br, opts)
	case PTypeUint16:
		return readFSSTWithOffsets[uint16](h, tableRaw, codes, table, offsetsHeader, br, opts)
	case PTypeUint32:
		return readFSSTWithOffsets[uint32](h, tableRaw, codes, table, offsetsHeader, br, opts)
	case PTypeUint64:
		return readFSSTWithOffsets[uint64](h, tableRaw, codes, table, offsetsHeader, br, opts)
	default:
		return nil, fmt.Errorf("codec: fsst offsets child type = %v, want unsigned integer", offsetsHeader.ElemType)
	}
}

func readAnyFSSTArray[T Integer | Float | String](br *array.BufReader, h codecHeader, opts ReadOptions) (EncodedArray[T], error) {
	var zero T
	switch any(zero).(type) {
	case string:
		return readCast[T](readFSSTArray(br, h, opts))
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

	// Collect string views for training (zero-copy via unsafe.Slice).
	inputs := make([][]byte, n)
	for i := uint64(0); i < n; i++ {
		s := arr.ValueAt(i)
		if len(s) > 0 {
			inputs[i] = unsafe.Slice(unsafe.StringData(s), len(s))
		}
	}

	table := fsst.Train(inputs)

	// Single pass: encode strings, track max offset and max length to
	// determine the narrowest integer width for both arrays.
	var allCodes []byte
	var maxOff, maxLen uint64
	offsets := make([]uint64, n+1)
	for i := uint64(0); i < n; i++ {
		encoded := table.Encode(inputs[i])
		offsets[i] = uint64(len(allCodes))
		allCodes = append(allCodes, encoded...)
		l := uint64(len(inputs[i]))
		if l > maxLen {
			maxLen = l
		}
	}
	offsets[n] = uint64(len(allCodes))
	maxOff = offsets[n]

	tableRaw, err := table.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("codec: fsst marshal table: %w", err)
	}

	// Build narrow offsets and lengths independently — offsets are sized by
	// maxOff (total compressed bytes), lengths by maxLen (max original string
	// length). For short-string workloads maxLen << maxOff, saving 2-3x on
	// lengths metadata.
	childCtx := ctx.descend()
	switch {
	case maxOff <= uint64(^uint8(0)):
		return buildFSSTWithOffsets[uint8](n, tableRaw, allCodes, table, offsets, inputs, maxLen, childCtx)
	case maxOff <= uint64(^uint16(0)):
		return buildFSSTWithOffsets[uint16](n, tableRaw, allCodes, table, offsets, inputs, maxLen, childCtx)
	case maxOff <= uint64(^uint32(0)):
		return buildFSSTWithOffsets[uint32](n, tableRaw, allCodes, table, offsets, inputs, maxLen, childCtx)
	default:
		return buildFSSTWithOffsets[uint64](n, tableRaw, allCodes, table, offsets, inputs, maxLen, childCtx)
	}
}

// buildFSSTWithOffsets narrows offsets to type I, then dispatches on maxLen to
// pick the narrowest length type J independently.
func buildFSSTWithOffsets[I UnsignedInteger](n uint64, tableRaw, allCodes []byte, table *fsst.Table, offsets []uint64, inputs [][]byte, maxLen uint64, ctx planContext) (EncodedArray[string], error) {
	narrowOff := make([]I, len(offsets))
	for i, v := range offsets {
		narrowOff[i] = I(v)
	}
	offsetsCodec, err := compressArray(array.NewPrimitivesUnsafe(narrowOff), ctx)
	if err != nil {
		return nil, err
	}
	switch {
	case maxLen <= uint64(^uint8(0)):
		return buildFSSTWithLengths[I, uint8](n, tableRaw, allCodes, table, offsetsCodec, inputs, ctx)
	case maxLen <= uint64(^uint16(0)):
		return buildFSSTWithLengths[I, uint16](n, tableRaw, allCodes, table, offsetsCodec, inputs, ctx)
	case maxLen <= uint64(^uint32(0)):
		return buildFSSTWithLengths[I, uint32](n, tableRaw, allCodes, table, offsetsCodec, inputs, ctx)
	default:
		return buildFSSTWithLengths[I, uint64](n, tableRaw, allCodes, table, offsetsCodec, inputs, ctx)
	}
}

// buildFSSTWithLengths narrows lengths to type J and assembles the final fsstArray.
func buildFSSTWithLengths[I, J UnsignedInteger](n uint64, tableRaw, allCodes []byte, table *fsst.Table, offsetsCodec EncodedArray[I], inputs [][]byte, ctx planContext) (EncodedArray[string], error) {
	narrowLen := make([]J, n)
	for i := uint64(0); i < n; i++ {
		narrowLen[i] = J(len(inputs[i]))
	}
	lengthsCodec, err := compressArray(array.NewPrimitivesUnsafe(narrowLen), ctx)
	if err != nil {
		return nil, err
	}
	return &fsstArray[I, J]{
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
