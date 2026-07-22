package codec

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
	encodedNode
	denseRows
	decodeLimit
	tableRaw []byte          // serialized fsst.Table
	codes    []byte          // concatenated compressed string bytes
	offsets  EncodedArray[I] // length+1 offsets into codes
	lengths  EncodedArray[J] // length original uncompressed string lengths
	table    *fsst.Table
	// decodedPayload is the total decoded string payload in bytes, and
	// decodeScratch the reusable decode buffer DecompressInto holds live beside
	// it: eight bytes per byte of the widest code span, FSST's expansion bound.
	// Both are derived, not serialized — the builder computes them from its
	// inputs and validatePayload from the children it has already checked — so
	// DecodedBytes never has to decode a child to answer.
	decodedPayload uint64
	decodeScratch  uint64
}

func (f *fsstArray[I, J]) CodecType() CodecType { return CodecTypeFSST }
func (f *fsstArray[I, J]) PType() PType         { return array.PTypeString }

func (f *fsstArray[I, J]) BinarySize() uint64 {
	return uint64(headerSize) + 4 + uint64(len(f.tableRaw)) + 4 + uint64(len(f.codes)) + f.offsets.BinarySize() + f.lengths.BinarySize()
}

func (f *fsstArray[I, J]) MarshalBinary() ([]byte, error) { return marshalBinary(f) }

// DecodedBytes reports the string header per element, the payload buffer they
// point into, the decode buffer the fill reuses across spans, and the offsets
// and lengths children DecompressInto decodes first. Counting only the payload
// understates a 33M-element array by 512 MiB, and omitting the decode buffer
// hides a scratch allocation that a crafted span can drive to the whole limit.
func (f *fsstArray[I, J]) DecodedBytes() (uint64, error) {
	var footprint decodeFootprint
	footprint.add(decodedBytesFor(f.Length(), array.PTypeString))
	footprint.add(f.decodedPayload, nil)
	footprint.add(f.decodeScratch, nil)
	footprint.add(f.offsets.DecodedBytes())
	footprint.add(f.lengths.DecodedBytes())
	return footprint.result()
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

// validatePayload decodes the offsets and lengths children once each and checks
// the invariants ValueAt relies on: monotonic offsets bounded by len(codes).
// Called on the read path so single-value access can never slice out of range
// on crafted input; DecompressInto re-validates per element. It records the
// decoded payload total and the widest code span it sees, so DecodedBytes need
// not recompute either.
func (f *fsstArray[I, J]) validatePayload(opts *readOptions) error {
	offsets, err := scanChild(f.offsets, opts)
	if err != nil {
		return fmt.Errorf("codec: decompress fsst offsets: %w", err)
	}
	lengths, err := scanChild(f.lengths, opts)
	if err != nil {
		return fmt.Errorf("codec: decompress fsst lengths: %w", err)
	}
	maxDecodedBytes := f.maxDecodedBytes()
	codesLen := uint64(len(f.codes))
	var decodedBytes, maxSpan uint64
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
		maxSpan = max(maxSpan, span)
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
	f.decodedPayload = decodedBytes
	f.decodeScratch = fsstDecodeScratch(maxSpan)
	return nil
}

// fsstDecodeScratch is the byte size of the reusable decode buffer one fill
// needs for code spans of up to maxSpan bytes. FSST expands at most 8x, and
// fsst.Table.Decode reallocates unless the buffer also covers its own 4n+8
// estimate, so covering both means the buffer is allocated exactly once per
// fill — which is what lets DecodedBytes report it as a fixed cost.
func fsstDecodeScratch(maxSpan uint64) uint64 { return 8*maxSpan + 8 }

// DecompressInto decodes FSST-compressed strings into dst. It uses per-string
// Decode into a single pre-sized output buffer rather than DecodeAll, which
// allocates per string. Each dst[i] aliases that shared buffer via unsafe.String.
// The reusable decode buffer is sized once, to the widest span's 8x worst case,
// so peak memory equals the decodeScratch DecodedBytes reports rather than
// whatever the row order happens to demand.
func (f *fsstArray[I, J]) DecompressInto(dst []string) error {
	if err := checkDstLen(dst, f.Length()); err != nil {
		return err
	}
	lengths, err := decompress(f.lengths, f.maxDecodedBytes())
	if err != nil {
		return err
	}
	offsets, err := decompress(f.offsets, f.maxDecodedBytes())
	if err != nil {
		return err
	}

	totalLen := uint64(0)
	maxSpan := uint64(0)
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
		maxSpan = max(maxSpan, codeEnd-codeStart)
	}
	outBuf := make([]byte, totalLen)
	decodeBuffer := make([]byte, fsstDecodeScratch(maxSpan))

	pos := uint64(0)
	for i := range f.Length() {
		l := uint64(lengths[i])
		if l == 0 {
			dst[i] = ""
		} else {
			codeStart := uint64(offsets[i])
			codeEnd := uint64(offsets[i+1])
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
	return sliceByDecoding[string](f, f.decodeLimit, start, end, sliceStringToRawArray)
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
func readFSSTWithOffsets[I UnsignedInteger](h codecHeader, tableRaw, codes []byte, table *fsst.Table, offsetsHeader codecHeader, br *array.BufReader, opts *readOptions) (EncodedArray[string], error) {
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

func readFSSTWithLengthsTyped[I, J UnsignedInteger](h codecHeader, tableRaw, codes []byte, table *fsst.Table, offsets EncodedArray[I], br *array.BufReader, lengthsHeader codecHeader, opts *readOptions) (EncodedArray[string], error) {
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
		denseRows:   denseRows(h.Length),
		decodeLimit: opts.decodeLimit(),
		tableRaw:    tableRaw,
		codes:       codes,
		offsets:     offsets,
		lengths:     lengths,
		table:       table,
	}
	if err := f.validatePayload(opts); err != nil {
		return nil, err
	}
	return f, nil
}

func readFSSTArray(br *array.BufReader, h codecHeader, opts *readOptions) (EncodedArray[string], error) {
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
func encodeFSST(arr array.ArrayCore[string], offsetsChildren, lengthsChildren UnsignedChildBuilder, budget buildBudget) (EncodedArray[string], error) {
	n := arr.Length()
	if n == 0 {
		return nil, errDataEmpty
	}
	if offsetsChildren == nil || lengthsChildren == nil {
		return nil, ErrBuilderRequired
	}

	if n == ^uint64(0) {
		return nil, fmt.Errorf("%w: FSST offsets length overflows", ErrMaterializationLimit)
	}
	if err := checkBuildSlice[[]byte](budget, n, "FSST input views"); err != nil {
		return nil, err
	}
	if err := checkBuildSlice[uint64](budget, n+1, "FSST offsets"); err != nil {
		return nil, err
	}

	var totalInputBytes, maxInputBytes uint64
	for i := range n {
		length := uint64(len(arr.ValueAt(i)))
		if length > budget.maxBytes-totalInputBytes {
			return nil, fmt.Errorf("%w: FSST input payload exceeds %d bytes", ErrMaterializationLimit, budget.maxBytes)
		}
		totalInputBytes += length
		maxInputBytes = max(maxInputBytes, length)
	}
	// fsst.Table.Encode may allocate 2*len(input)+7 bytes for its reusable
	// output buffer. Check that exact bound before handing it an input.
	if maxInputBytes > (budget.maxBytes-7)/2 {
		return nil, fmt.Errorf("%w: FSST encode buffer exceeds %d bytes", ErrMaterializationLimit, budget.maxBytes)
	}

	// Collect string views for training (zero-copy via unsafe.Slice).
	inputs, err := makeBuildSlice[[]byte](budget, n, n, "FSST input views")
	if err != nil {
		return nil, err
	}
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
	initialCodesCapacity := totalInputBytes/2 + 1
	allCodes, err := makeBuildSlice[byte](budget, 0, initialCodesCapacity, "FSST codes")
	if err != nil {
		return nil, err
	}
	var encodeBuffer []byte
	var maxOff, maxLen uint64
	offsets, err := makeBuildSlice[uint64](budget, n+1, n+1, "FSST offsets")
	if err != nil {
		return nil, err
	}
	for i := range n {
		offsets[i] = uint64(len(allCodes))
		encoded := table.EncodeInto(encodeBuffer[:0], inputs[i])
		allCodes, err = appendBuildSlice(allCodes, encoded, budget, "FSST codes")
		if err != nil {
			return nil, err
		}
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
		return buildFSSTWithOffsets(n, tableRaw, allCodes, table, offsets, inputs, maxLen, offsetsChildren.BuildUint8, lengthsChildren, budget)
	case maxOff <= uint64(^uint16(0)):
		return buildFSSTWithOffsets(n, tableRaw, allCodes, table, offsets, inputs, maxLen, offsetsChildren.BuildUint16, lengthsChildren, budget)
	case maxOff <= uint64(^uint32(0)):
		return buildFSSTWithOffsets(n, tableRaw, allCodes, table, offsets, inputs, maxLen, offsetsChildren.BuildUint32, lengthsChildren, budget)
	default:
		return buildFSSTWithOffsets(n, tableRaw, allCodes, table, offsets, inputs, maxLen, offsetsChildren.BuildUint64, lengthsChildren, budget)
	}
}

// buildFSSTWithOffsets narrows offsets to type I, then dispatches on maxLen to
// pick the narrowest length type J independently.
func buildFSSTWithOffsets[I UnsignedInteger](n uint64, tableRaw, allCodes []byte, table *fsst.Table, offsets []uint64, inputs [][]byte, maxLen uint64, buildOffsets ChildBuilder[I], lengthsChildren UnsignedChildBuilder, budget buildBudget) (EncodedArray[string], error) {
	narrowOff, err := makeBuildSlice[I](budget, uint64(len(offsets)), uint64(len(offsets)), "FSST narrowed offsets")
	if err != nil {
		return nil, err
	}
	for i, v := range offsets {
		narrowOff[i] = I(v)
	}
	offsetsCodec, err := buildOffsets(array.NewPrimitivesUnsafe(narrowOff))
	offsetsCodec, err = adoptChild(uint64(len(narrowOff)), offsetsCodec, err)
	if err != nil {
		return nil, fmt.Errorf("codec: compress FSST offsets: %w", err)
	}
	var maxSpan uint64
	for i := 1; i < len(offsets); i++ {
		maxSpan = max(maxSpan, offsets[i]-offsets[i-1])
	}
	scratch := fsstDecodeScratch(maxSpan)
	switch {
	case maxLen <= uint64(^uint8(0)):
		return buildFSSTWithLengths(n, tableRaw, allCodes, table, offsetsCodec, inputs, scratch, lengthsChildren.BuildUint8, budget)
	case maxLen <= uint64(^uint16(0)):
		return buildFSSTWithLengths(n, tableRaw, allCodes, table, offsetsCodec, inputs, scratch, lengthsChildren.BuildUint16, budget)
	case maxLen <= uint64(^uint32(0)):
		return buildFSSTWithLengths(n, tableRaw, allCodes, table, offsetsCodec, inputs, scratch, lengthsChildren.BuildUint32, budget)
	default:
		return buildFSSTWithLengths(n, tableRaw, allCodes, table, offsetsCodec, inputs, scratch, lengthsChildren.BuildUint64, budget)
	}
}

// buildFSSTWithLengths narrows lengths to type J and assembles the final fsstArray.
func buildFSSTWithLengths[I, J UnsignedInteger](n uint64, tableRaw, allCodes []byte, table *fsst.Table, offsetsCodec EncodedArray[I], inputs [][]byte, decodeScratch uint64, buildLengths ChildBuilder[J], budget buildBudget) (EncodedArray[string], error) {
	narrowLen, err := makeBuildSlice[J](budget, n, n, "FSST lengths")
	if err != nil {
		return nil, err
	}
	var decodedPayload uint64
	for i := range n {
		narrowLen[i] = J(len(inputs[i]))
		decodedPayload += uint64(len(inputs[i]))
	}
	lengthsCodec, err := buildLengths(array.NewPrimitivesUnsafe(narrowLen))
	lengthsCodec, err = adoptChild(n, lengthsCodec, err)
	if err != nil {
		return nil, fmt.Errorf("codec: compress FSST lengths: %w", err)
	}
	return &fsstArray[I, J]{
		denseRows:      denseRows(n),
		tableRaw:       tableRaw,
		codes:          allCodes,
		offsets:        offsetsCodec,
		lengths:        lengthsCodec,
		table:          table,
		decodedPayload: decodedPayload,
		decodeScratch:  decodeScratch,
	}, nil
}
