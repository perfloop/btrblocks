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
	denseRows
	tableRaw []byte          // serialized fsst.Table
	codes    []byte          // concatenated compressed string bytes
	offsets  EncodedArray[I] // length+1 offsets into codes
	lengths  EncodedArray[J] // length original uncompressed string lengths
	table    *fsst.Table
}

func (f *fsstArray[I, J]) CodecType() CodecType { return CodecTypeFSST }
func (f *fsstArray[I, J]) PType() PType         { return array.PTypeString }

func (f *fsstArray[I, J]) BinarySize() uint64 {
	return uint64(headerSize) + 4 + uint64(len(f.tableRaw)) + 4 + uint64(len(f.codes)) + f.offsets.BinarySize() + f.lengths.BinarySize()
}

func (f *fsstArray[I, J]) DecodedBytes() (uint64, error) {
	var total uint64
	for i := range f.lengths.Length() {
		length := uint64(f.lengths.ValueAt(i))
		if length > ^uint64(0)-total {
			return 0, fmt.Errorf("codec: fsst decoded length overflows at position %d", i)
		}
		total += length
	}
	return total, nil
}

func (f *fsstArray[I, J]) ValueAt(offset uint64) string {
	if offset >= f.Length() {
		panic(errOffsetOutOfRange)
	}
	start := uint64(f.offsets.ValueAt(offset))
	end := uint64(f.offsets.ValueAt(offset + 1))
	if start >= end {
		// start > end cannot occur for arrays built or read by this package:
		// the builder emits monotonic offsets and the read path validates them.
		return ""
	}
	decoded := f.table.DecodeAll(f.codes[start:end])
	if len(decoded) == 0 {
		return ""
	}
	return unsafe.String(&decoded[0], len(decoded))
}

// validateOffsets decodes the offsets child once and checks the invariants
// ValueAt relies on: monotonic offsets bounded by len(codes). Called on the
// read path so single-value access can never slice out of range on crafted
// input; DecompressInto re-validates per element.
func (f *fsstArray[I, J]) validatePayload(maxDecodedBytes uint64) error {
	offsets, err := Decompress(f.offsets)
	if err != nil {
		return fmt.Errorf("codec: decompress fsst offsets: %w", err)
	}
	lengths, err := Decompress(f.lengths)
	if err != nil {
		return fmt.Errorf("codec: decompress fsst lengths: %w", err)
	}
	codesLen := uint64(len(f.codes))
	var decodedBytes uint64
	var decodeBuffer []byte
	var decodedOffsets []int
	var sourceOffsets [2]int
	for i, lengthValue := range lengths {
		start := uint64(offsets[i])
		end := uint64(offsets[i+1])
		if start > end || end > codesLen {
			return fmt.Errorf("codec: fsst offset [%d, %d) at position %d outside [0, %d)", start, end, i, codesLen)
		}
		span := end - start
		if span > maxDecodedBytes/8 {
			return fmt.Errorf("codec: fsst code span %d at position %d exceeds decoded-byte limit %d", span, i, maxDecodedBytes)
		}
		length := uint64(lengthValue)
		if length > 8*span {
			return fmt.Errorf("codec: fsst length %d at position %d exceeds decodable span %d", length, i, 8*span)
		}
		sourceOffsets[1] = int(span)
		decoded, offsets, err := f.table.DecodeBatch(decodeBuffer[:0], decodedOffsets[:0], f.codes[start:end], sourceOffsets[:])
		if err != nil {
			return fmt.Errorf("codec: decode fsst span at position %d: %w", i, err)
		}
		decodeBuffer = decoded
		decodedOffsets = offsets
		if actual := uint64(len(decoded)); actual != length {
			return fmt.Errorf("codec: fsst decoded length = %d, want %d at position %d", actual, length, i)
		}
		if length > maxDecodedBytes-decodedBytes {
			return fmt.Errorf("codec: fsst decoded bytes exceed limit %d", maxDecodedBytes)
		}
		decodedBytes += length
	}
	return nil
}

// DecompressInto decodes FSST-compressed strings into dst. It uses per-string
// Decode into a single pre-sized output buffer rather than DecodeAll, which
// allocates per string. Each dst[i] aliases that shared buffer via unsafe.String.
func (f *fsstArray[I, J]) DecompressInto(dst []string) error {
	if err := checkDstLen(dst, f.Length()); err != nil {
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
	codesLen := uint64(len(f.codes))
	for i, l := range lengths {
		length := uint64(l)
		codeStart := uint64(offsets[i])
		codeEnd := uint64(offsets[i+1])
		if codeStart > codeEnd || codeEnd > codesLen {
			return fmt.Errorf("codec: fsst offset [%d, %d) out of range [0, %d) at position %d", codeStart, codeEnd, codesLen, i)
		}
		// FSST symbols are at most 8 bytes, so a span of n code bytes decodes to
		// at most 8n bytes. Bounding each length against its span holds totalLen
		// below 8*len(codes), so a crafted lengths child cannot drive an
		// unbounded (or overflowing) outBuf allocation before decoding validates.
		if length > 8*(codeEnd-codeStart) {
			return fmt.Errorf("codec: fsst length %d at position %d exceeds decodable span %d", length, i, 8*(codeEnd-codeStart))
		}
		totalLen += length
	}
	outBuf := make([]byte, totalLen)
	var decodeBuffer []byte

	pos := uint64(0)
	for i := range f.Length() {
		l := uint64(lengths[i])
		if l == 0 {
			dst[i] = ""
		} else {
			codeStart := uint64(offsets[i])
			codeEnd := uint64(offsets[i+1])
			// Decode panics when dst is too small, so size it to the 8x
			// worst case rather than the untrusted stored length.
			if need := 8 * (codeEnd - codeStart); uint64(len(decodeBuffer)) < need {
				decodeBuffer = make([]byte, need)
			}
			decoded := f.table.DecodeInto(decodeBuffer[:0], f.codes[codeStart:codeEnd])
			n := len(decoded)
			decodeBuffer = decoded[:cap(decoded)]
			if uint64(n) != l {
				return fmt.Errorf("codec: fsst decoded length = %d, want %d at position %d", n, l, i)
			}
			buf := outBuf[pos : pos+l]
			copy(buf, decodeBuffer[:n])
			dst[i] = unsafe.String(unsafe.SliceData(buf), len(buf))
		}
		pos += l
	}
	return nil
}

func (f *fsstArray[I, J]) Slice(start, end uint64) (EncodedArray[string], error) {
	return sliceStringToRawArray(f, start, end)
}

func (f *fsstArray[I, J]) WriteTo(w io.Writer) (int64, error) {
	bodySize := uint64(4) + uint64(len(f.tableRaw)) + uint64(4) + uint64(len(f.codes))
	sum := writeSum{w: w}
	if err := sum.writeTo(codecHeader{
		Version:  versionNumber,
		Type:     CodecTypeFSST,
		ElemType: array.PTypeString,
		Length:   f.Length(),
		NumBytes: bodySize,
	}); err != nil {
		return sum.n, err
	}

	var buf4 [4]byte

	// table size + table bytes
	binary.LittleEndian.PutUint32(buf4[:], uint32(len(f.tableRaw)))
	if err := sum.write(buf4[:]); err != nil {
		return sum.n, err
	}
	if err := sum.write(f.tableRaw); err != nil {
		return sum.n, err
	}

	// codes size + codes bytes
	binary.LittleEndian.PutUint32(buf4[:], uint32(len(f.codes)))
	if err := sum.write(buf4[:]); err != nil {
		return sum.n, err
	}
	if err := sum.write(f.codes); err != nil {
		return sum.n, err
	}

	// offsets child
	if err := sum.writeTo(f.offsets); err != nil {
		return sum.n, err
	}

	// lengths child
	if err := sum.writeTo(f.lengths); err != nil {
		return sum.n, err
	}
	return sum.n, nil
}

// readFSSTWithOffsets reads offsets, then dispatches on lengths ElemType.
func readFSSTWithOffsets[I UnsignedInteger](h codecHeader, tableRaw, codes []byte, table *fsst.Table, offsetsHeader codecHeader, br *array.BufReader, opts ReadOptions) (EncodedArray[string], error) {
	offsets, err := readUnsignedEncodedArrayWithHeader[I](br, offsetsHeader, opts)
	if err != nil {
		return nil, fmt.Errorf("codec: fsst offsets: %w", err)
	}
	if err := requireNonNullable(offsets, "fsst offsets"); err != nil {
		return nil, err
	}
	if offsets.Length() != h.Length+1 {
		return nil, fmt.Errorf("codec: fsst offsets length = %d, want %d", offsets.Length(), h.Length+1)
	}
	lengthsHeader, err := readHeader(br)
	if err != nil {
		return nil, fmt.Errorf("codec: fsst lengths header: %w", err)
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
	lengths, err := readUnsignedEncodedArrayWithHeader[J](br, lengthsHeader, opts)
	if err != nil {
		return nil, fmt.Errorf("codec: fsst lengths: %w", err)
	}
	if err := requireNonNullable(lengths, "fsst lengths"); err != nil {
		return nil, err
	}
	if lengths.Length() != h.Length {
		return nil, fmt.Errorf("codec: fsst lengths length = %d, want %d", lengths.Length(), h.Length)
	}
	f := &fsstArray[I, J]{
		denseRows: denseRows(h.Length),
		tableRaw:  tableRaw,
		codes:     codes,
		offsets:   offsets,
		lengths:   lengths,
		table:     table,
	}
	if err := f.validatePayload(opts.DecodedByteLimit()); err != nil {
		return nil, err
	}
	return f, nil
}

func readFSSTArray(br *array.BufReader, h codecHeader, opts ReadOptions) (EncodedArray[string], error) {
	// Body must hold at least two 4-byte length prefixes.
	if h.NumBytes < 8 {
		return nil, fmt.Errorf("codec: fsst body size %d too small", h.NumBytes)
	}

	// read table
	data, err := br.Read(4)
	if err != nil {
		return nil, fmt.Errorf("codec: fsst table size: %w", err)
	}
	tableSize := binary.LittleEndian.Uint32(data)

	// Validate sizes against h.NumBytes before allocating.
	if uint64(4)+uint64(tableSize)+uint64(4) > h.NumBytes {
		return nil, fmt.Errorf("codec: fsst table size %d exceeds body", tableSize)
	}

	tableRaw, err := br.Read(int(tableSize))
	if err != nil {
		return nil, fmt.Errorf("codec: fsst table: %w", err)
	}

	// read codes
	data, err = br.Read(4)
	if err != nil {
		return nil, fmt.Errorf("codec: fsst codes size: %w", err)
	}
	codesSize := binary.LittleEndian.Uint32(data)

	if expectedBody := uint64(4) + uint64(tableSize) + uint64(4) + uint64(codesSize); h.NumBytes != expectedBody {
		return nil, fmt.Errorf("codec: fsst body size = %d, want %d", h.NumBytes, expectedBody)
	}

	codes, err := br.Read(int(codesSize))
	if err != nil {
		return nil, fmt.Errorf("codec: fsst codes: %w", err)
	}

	table := &fsst.Table{}
	if err := table.UnmarshalBinary(tableRaw); err != nil {
		return nil, fmt.Errorf("codec: fsst table: %w", err)
	}

	// read offsets header
	offsetsHeader, err := readHeader(br)
	if err != nil {
		return nil, fmt.Errorf("codec: fsst offsets header: %w", err)
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

// encodeFSST trains and applies FSST while leaving offsets and
// lengths child selection to the caller.
func encodeFSST(arr array.ArrayCore[string], offsetsChildren, lengthsChildren unsignedChildBuilder) (EncodedArray[string], error) {
	n := arr.Length()
	if n == 0 {
		return nil, errDataEmpty
	}
	if offsetsChildren == nil || lengthsChildren == nil {
		return nil, ErrBuilderRequired
	}

	// Collect string views for training (zero-copy via unsafe.Slice).
	inputs := make([][]byte, n)
	for i := range n {
		s := arr.ValueAt(i)
		if len(s) > 0 {
			inputs[i] = unsafe.Slice(unsafe.StringData(s), len(s))
		}
	}

	table := fsst.Train(inputs)

	// Single pass: encode strings, track max offset and max length to
	// determine the narrowest integer width for both arrays.
	// Pre-allocate allCodes assuming FSST compresses to ~50% of input.
	var totalInputBytes int
	for _, in := range inputs {
		totalInputBytes += len(in)
	}
	allCodes := make([]byte, 0, totalInputBytes/2+1)
	var encodeBuffer []byte
	var maxOff, maxLen uint64
	offsets := make([]uint64, n+1)
	for i := range n {
		offsets[i] = uint64(len(allCodes))
		encoded := table.EncodeInto(encodeBuffer[:0], inputs[i])
		allCodes = append(allCodes, encoded...)
		encodeBuffer = encoded[:0]
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
	switch {
	case maxOff <= uint64(^uint8(0)):
		return buildFSSTWithOffsets[uint8](n, tableRaw, allCodes, table, offsets, inputs, maxLen, offsetsChildren.BuildUint8, lengthsChildren)
	case maxOff <= uint64(^uint16(0)):
		return buildFSSTWithOffsets[uint16](n, tableRaw, allCodes, table, offsets, inputs, maxLen, offsetsChildren.BuildUint16, lengthsChildren)
	case maxOff <= uint64(^uint32(0)):
		return buildFSSTWithOffsets[uint32](n, tableRaw, allCodes, table, offsets, inputs, maxLen, offsetsChildren.BuildUint32, lengthsChildren)
	default:
		return buildFSSTWithOffsets[uint64](n, tableRaw, allCodes, table, offsets, inputs, maxLen, offsetsChildren.BuildUint64, lengthsChildren)
	}
}

// buildFSSTWithOffsets narrows offsets to type I, then dispatches on maxLen to
// pick the narrowest length type J independently.
func buildFSSTWithOffsets[I UnsignedInteger](n uint64, tableRaw, allCodes []byte, table *fsst.Table, offsets []uint64, inputs [][]byte, maxLen uint64, buildOffsets childBuilder[I], lengthsChildren unsignedChildBuilder) (EncodedArray[string], error) {
	narrowOff := make([]I, len(offsets))
	for i, v := range offsets {
		narrowOff[i] = I(v)
	}
	offsetsCodec, err := buildOffsets(array.NewPrimitivesUnsafe(narrowOff))
	if err != nil {
		return nil, err
	}
	switch {
	case maxLen <= uint64(^uint8(0)):
		return buildFSSTWithLengths[I, uint8](n, tableRaw, allCodes, table, offsetsCodec, inputs, lengthsChildren.BuildUint8)
	case maxLen <= uint64(^uint16(0)):
		return buildFSSTWithLengths[I, uint16](n, tableRaw, allCodes, table, offsetsCodec, inputs, lengthsChildren.BuildUint16)
	case maxLen <= uint64(^uint32(0)):
		return buildFSSTWithLengths[I, uint32](n, tableRaw, allCodes, table, offsetsCodec, inputs, lengthsChildren.BuildUint32)
	default:
		return buildFSSTWithLengths[I, uint64](n, tableRaw, allCodes, table, offsetsCodec, inputs, lengthsChildren.BuildUint64)
	}
}

// buildFSSTWithLengths narrows lengths to type J and assembles the final fsstArray.
func buildFSSTWithLengths[I, J UnsignedInteger](n uint64, tableRaw, allCodes []byte, table *fsst.Table, offsetsCodec EncodedArray[I], inputs [][]byte, buildLengths childBuilder[J]) (EncodedArray[string], error) {
	narrowLen := make([]J, n)
	for i := range n {
		narrowLen[i] = J(len(inputs[i]))
	}
	lengthsCodec, err := buildLengths(array.NewPrimitivesUnsafe(narrowLen))
	if err != nil {
		return nil, err
	}
	return &fsstArray[I, J]{
		denseRows: denseRows(n),
		tableRaw:  tableRaw,
		codes:     allCodes,
		offsets:   offsetsCodec,
		lengths:   lengthsCodec,
		table:     table,
	}, nil
}
