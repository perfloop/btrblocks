package btrblocks

import (
	"errors"
	"fmt"
	"io"
	"math/bits"

	"github.com/axiomhq/btrblocks/array"
)

const flagSparseBitmap uint32 = 1 << 0

// sparseArray is a physical encoding of a dense logical array. It stores the
// most frequent value once and the positions and values that differ from it.
// Nullability is not represented here; callers own logical validity.
type sparseArray[V Integer | Float | String, I UnsignedInteger] struct {
	denseRows
	fill    EncodedArray[V]
	indices EncodedArray[I]
	values  EncodedArray[V]
	slice   sliceBuilder[V]
}

func (s *sparseArray[V, I]) CodecType() CodecType {
	return CodecTypeSparse
}
func (s *sparseArray[V, I]) PType() PType { return s.fill.PType() }
func (s *sparseArray[V, I]) DecodedBytes() (uint64, error) {
	return decodedBytesFor(s.Length(), s.PType())
}

func (s *sparseArray[V, I]) NumSparseValues() uint64 { return s.values.Length() }
func (s *sparseArray[V, I]) FillValue() V            { return s.fill.ValueAt(0) }

func (s *sparseArray[V, I]) VisitSparseValues(visit func(index uint64, value V) error) error {
	for i := range s.indices.Length() {
		if err := visit(uint64(s.indices.ValueAt(i)), s.values.ValueAt(i)); err != nil {
			return err
		}
	}
	return nil
}

func (s *sparseArray[V, I]) BinarySize() uint64 {
	return uint64(headerSize) + s.fill.BinarySize() + s.indices.BinarySize() + s.values.BinarySize()
}

func (s *sparseArray[V, I]) ValueAt(offset uint64) V {
	if offset >= s.Length() {
		panic(errOffsetOutOfRange)
	}
	lo, hi := uint64(0), s.indices.Length()
	for lo < hi {
		mid := lo + (hi-lo)/2
		index := uint64(s.indices.ValueAt(mid))
		if index < offset {
			lo = mid + 1
			continue
		}
		if index > offset {
			hi = mid
			continue
		}
		return s.values.ValueAt(mid)
	}
	return s.fill.ValueAt(0)
}

func (s *sparseArray[V, I]) DecompressInto(dst []V) error {
	if err := checkDstLen(dst, s.Length()); err != nil {
		return err
	}
	fill := s.fill.ValueAt(0)
	for i := range s.Length() {
		dst[i] = fill
	}
	indices, err := Decompress(s.indices)
	if err != nil {
		return fmt.Errorf("codec: decompress sparse indices: %w", err)
	}
	values, err := Decompress(s.values)
	if err != nil {
		return fmt.Errorf("codec: decompress sparse values: %w", err)
	}
	for i, index := range indices {
		dst[index] = values[i]
	}
	return nil
}

func (s *sparseArray[V, I]) Slice(start, end uint64) (EncodedArray[V], error) {
	return s.slice(s, start, end)
}

func (s *sparseArray[V, I]) WriteTo(w io.Writer) (int64, error) {
	sum := writeSum{w: w}
	if err := sum.writeTo(codecHeader{
		Version:  versionNumber,
		Type:     CodecTypeSparse,
		ElemType: s.fill.PType(),
		Length:   s.Length(),
		NumBytes: 0,
	}); err != nil {
		return sum.n, err
	}
	for _, child := range []io.WriterTo{s.fill, s.indices, s.values} {
		if err := sum.writeTo(child); err != nil {
			return sum.n, err
		}
	}
	return sum.n, nil
}

func readSparseArray[V Integer | Float | String](br *array.BufReader, h codecHeader, opts ReadOptions, readValues encodedReader[V], slice sliceBuilder[V]) (EncodedArray[V], error) {
	if h.Flags&^flagSparseBitmap != 0 {
		return nil, fmt.Errorf("codec: unsupported sparse flags = 0x%x", h.Flags)
	}
	if h.Flags == flagSparseBitmap {
		bitmapSize := (h.Length + 7) / 8
		if h.NumBytes != bitmapSize {
			return nil, fmt.Errorf("codec: sparse bitmap body size = %d, want %d", h.NumBytes, bitmapSize)
		}
		if bitmapSize > uint64(^uint(0)>>1) {
			return nil, errors.New("codec: sparse bitmap exceeds platform limit")
		}
		bitmap, err := br.Read(int(bitmapSize))
		if err != nil {
			return nil, fmt.Errorf("codec: reading sparse bitmap: %w", err)
		}
		fill, err := readValues(br, opts)
		if err != nil {
			return nil, fmt.Errorf("codec: sparse fill: %w", err)
		}
		if err := requireNonNullable(fill, "sparse fill"); err != nil {
			return nil, err
		}
		values, err := readValues(br, opts)
		if err != nil {
			return nil, fmt.Errorf("codec: sparse values: %w", err)
		}
		if err := requireNonNullable(values, "sparse values"); err != nil {
			return nil, err
		}
		sparse := &bitmapSparseArray[V]{denseRows: denseRows(h.Length), fill: fill, bitmap: bitmap, values: values, slice: slice}
		if err := sparse.validate(); err != nil {
			return nil, err
		}
		return sparse, nil
	}
	if h.NumBytes != 0 {
		return nil, fmt.Errorf("codec: sparse body size = %d, want 0", h.NumBytes)
	}
	fill, err := readValues(br, opts)
	if err != nil {
		return nil, fmt.Errorf("codec: sparse fill: %w", err)
	}
	if fill.Length() != 1 {
		return nil, fmt.Errorf("codec: sparse fill length = %d, want 1", fill.Length())
	}
	if err := requireNonNullable(fill, "sparse fill"); err != nil {
		return nil, err
	}
	indexHeader, err := readHeader(br)
	if err != nil {
		return nil, fmt.Errorf("codec: reading sparse indices header: %w", err)
	}
	var result EncodedArray[V]
	switch indexHeader.ElemType {
	case PTypeUint8:
		result, err = readSparseChildren[V, uint8](br, h, indexHeader, opts, fill, readValues, slice)
	case PTypeUint16:
		result, err = readSparseChildren[V, uint16](br, h, indexHeader, opts, fill, readValues, slice)
	case PTypeUint32:
		result, err = readSparseChildren[V, uint32](br, h, indexHeader, opts, fill, readValues, slice)
	case PTypeUint64:
		result, err = readSparseChildren[V, uint64](br, h, indexHeader, opts, fill, readValues, slice)
	default:
		return nil, fmt.Errorf("codec: sparse index type = %v, want unsigned integer", indexHeader.ElemType)
	}
	return result, err
}

func readSparseChildren[V Integer | Float | String, I UnsignedInteger](br *array.BufReader, h, indexHeader codecHeader, opts ReadOptions, fill EncodedArray[V], readValues encodedReader[V], slice sliceBuilder[V]) (EncodedArray[V], error) {
	indices, err := readUnsignedEncodedArrayWithHeader[I](br, indexHeader, opts)
	if err != nil {
		return nil, fmt.Errorf("codec: sparse indices: %w", err)
	}
	if err := requireNonNullable(indices, "sparse indices"); err != nil {
		return nil, err
	}
	values, err := readValues(br, opts)
	if err != nil {
		return nil, fmt.Errorf("codec: sparse values: %w", err)
	}
	if err := requireNonNullable(values, "sparse values"); err != nil {
		return nil, err
	}
	if indices.Length() != values.Length() {
		return nil, fmt.Errorf("codec: sparse indices length = %d, values length = %d", indices.Length(), values.Length())
	}
	s := &sparseArray[V, I]{denseRows: denseRows(h.Length), fill: fill, indices: indices, values: values, slice: slice}
	if err := s.validate(h.Length); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *sparseArray[V, I]) validate(length uint64) error {
	if s.fill.Length() != 1 || s.Length() != length {
		return fmt.Errorf("codec: sparse length = %d, want %d", s.Length(), length)
	}
	var previous uint64
	for i := range s.indices.Length() {
		index := uint64(s.indices.ValueAt(i))
		if index >= length {
			return fmt.Errorf("codec: sparse index %d at position %d >= length %d", index, i, length)
		}
		if i > 0 && index <= previous {
			return fmt.Errorf("codec: sparse index %d at position %d is not strictly increasing", index, i)
		}
		previous = index
	}
	return nil
}

// bitmapSparseArray stores a dense logical array as one fill value, an
// exception bitmap, and densely packed exception values. The bitmap marks
// physical exceptions, not logical nulls.
type bitmapSparseArray[V Integer | Float | String] struct {
	denseRows
	fill   EncodedArray[V]
	bitmap []byte
	values EncodedArray[V]
	rank   []uint32
	slice  sliceBuilder[V]
}

func (s *bitmapSparseArray[V]) CodecType() CodecType {
	return CodecTypeSparse
}
func (s *bitmapSparseArray[V]) PType() PType { return s.fill.PType() }
func (s *bitmapSparseArray[V]) DecodedBytes() (uint64, error) {
	return decodedBytesFor(s.Length(), s.PType())
}

func (s *bitmapSparseArray[V]) NumSparseValues() uint64 { return s.values.Length() }
func (s *bitmapSparseArray[V]) FillValue() V            { return s.fill.ValueAt(0) }

func (s *bitmapSparseArray[V]) VisitSparseValues(visit func(index uint64, value V) error) error {
	patch := uint64(0)
	for byteIndex, valueBits := range s.bitmap {
		for valueBits != 0 {
			bit := bits.TrailingZeros8(valueBits)
			index := uint64(byteIndex*8 + bit)
			if index >= s.Length() {
				break
			}
			if err := visit(index, s.values.ValueAt(patch)); err != nil {
				return err
			}
			patch++
			valueBits &^= 1 << bit
		}
	}
	return nil
}

func (s *bitmapSparseArray[V]) BinarySize() uint64 {
	return uint64(headerSize) + uint64(len(s.bitmap)) + s.fill.BinarySize() + s.values.BinarySize()
}

func (s *bitmapSparseArray[V]) ValueAt(offset uint64) V {
	if offset >= s.Length() {
		panic(errOffsetOutOfRange)
	}
	byteIndex := offset / 8
	bit := byte(offset % 8)
	if s.bitmap[byteIndex]&(1<<bit) == 0 {
		return s.fill.ValueAt(0)
	}
	patch := uint64(s.rank[byteIndex]) + uint64(bits.OnesCount8(s.bitmap[byteIndex]&((1<<bit)-1)))
	return s.values.ValueAt(patch)
}

func (s *bitmapSparseArray[V]) DecompressInto(dst []V) error {
	if err := checkDstLen(dst, s.Length()); err != nil {
		return err
	}
	fill := s.fill.ValueAt(0)
	for i := range s.Length() {
		dst[i] = fill
	}
	return s.VisitSparseValues(func(index uint64, value V) error {
		dst[index] = value
		return nil
	})
}

func (s *bitmapSparseArray[V]) Slice(start, end uint64) (EncodedArray[V], error) {
	return s.slice(s, start, end)
}

func (s *bitmapSparseArray[V]) WriteTo(w io.Writer) (int64, error) {
	sum := writeSum{w: w}
	if err := sum.writeTo(codecHeader{
		Version:  versionNumber,
		Type:     CodecTypeSparse,
		ElemType: s.fill.PType(),
		Flags:    flagSparseBitmap,
		Length:   s.Length(),
		NumBytes: uint64(len(s.bitmap)),
	}); err != nil {
		return sum.n, err
	}
	if err := sum.write(s.bitmap); err != nil {
		return sum.n, err
	}
	for _, child := range []io.WriterTo{s.fill, s.values} {
		if err := sum.writeTo(child); err != nil {
			return sum.n, err
		}
	}
	return sum.n, nil
}

func (s *bitmapSparseArray[V]) validate() error {
	wantBitmap := (s.Length() + 7) / 8
	if uint64(len(s.bitmap)) != wantBitmap {
		return fmt.Errorf("codec: sparse bitmap size = %d, want %d", len(s.bitmap), wantBitmap)
	}
	if s.fill.Length() != 1 {
		return fmt.Errorf("codec: sparse fill length = %d, want 1", s.fill.Length())
	}
	if s.Length()%8 != 0 && len(s.bitmap) > 0 && s.bitmap[len(s.bitmap)-1]&^byte((1<<(s.Length()%8))-1) != 0 {
		return errors.New("codec: sparse bitmap has non-zero padding bits")
	}
	patches := uint64(0)
	for _, valueBits := range s.bitmap {
		patches += uint64(bits.OnesCount8(valueBits))
	}
	if patches != s.values.Length() {
		return fmt.Errorf("codec: sparse bitmap patches = %d, values = %d", patches, s.values.Length())
	}
	s.rank = make([]uint32, len(s.bitmap)+1)
	for i, valueBits := range s.bitmap {
		s.rank[i+1] = s.rank[i] + uint32(bits.OnesCount8(valueBits))
	}
	return nil
}

func buildBitmapSparse[T Integer | Float | String](length uint64, fill, values EncodedArray[T], indices []uint64, slice sliceBuilder[T]) (EncodedArray[T], error) {
	bitmap := make([]byte, (length+7)/8)
	for _, index := range indices {
		bitmap[index/8] |= 1 << (index % 8)
	}
	sparse := &bitmapSparseArray[T]{denseRows: denseRows(length), fill: fill, bitmap: bitmap, values: values, slice: slice}
	if err := sparse.validate(); err != nil {
		return nil, err
	}
	return sparse, nil
}

// sparseChildBuilder compresses the fill, patch values, and each supported
// patch-index width.
type sparseChildBuilder[T Integer | Float | String] interface {
	BuildFill(array.ArrayCore[T]) (EncodedArray[T], error)
	BuildValues(array.ArrayCore[T]) (EncodedArray[T], error)
	BuildUint8(array.ArrayCore[uint8]) (EncodedArray[uint8], error)
	BuildUint16(array.ArrayCore[uint16]) (EncodedArray[uint16], error)
	BuildUint32(array.ArrayCore[uint32]) (EncodedArray[uint32], error)
	BuildUint64(array.ArrayCore[uint64]) (EncodedArray[uint64], error)
}

// encodeIntegerSparse extracts an integer fill and patches and
// leaves child compression to the caller.
func encodeIntegerSparse[T Integer](arr array.ArrayCore[T], children sparseChildBuilder[T]) (EncodedArray[T], error) {
	return buildSparseWithKey(arr, func(value T) T { return value }, children, slicePrimitiveToRawArray[T])
}

// encodeIntegerSparseWithFill pins the zero value as the fill, sized for the
// non-null values of a null-dominated array.
func encodeIntegerSparseWithFill[T Integer](arr array.ArrayCore[T], nullCount uint64, children sparseChildBuilder[T]) (EncodedArray[T], error) {
	var fill T
	return buildSparseWithFill(arr, func(value T) T { return value }, fill, arr.Length()-min(nullCount, arr.Length()), children, slicePrimitiveToRawArray[T])
}

// encodeFloatSparse extracts a bitwise float fill and patches and
// leaves child compression to the caller. Bitwise equality preserves signed
// zero and distinct NaN payloads.
func encodeFloatSparse[T Float](arr array.ArrayCore[T], children sparseChildBuilder[T]) (EncodedArray[T], error) {
	return buildSparseWithKey(arr, array.FloatBits[T], children, slicePrimitiveToRawArray[T])
}

// encodeFloatSparseWithFill pins positive zero as the fill, sized for the
// non-null values of a null-dominated array. Bitwise equality preserves signed
// zero and distinct NaN payloads.
func encodeFloatSparseWithFill[T Float](arr array.ArrayCore[T], nullCount uint64, children sparseChildBuilder[T]) (EncodedArray[T], error) {
	var fill T
	return buildSparseWithFill(arr, array.FloatBits[T], fill, arr.Length()-min(nullCount, arr.Length()), children, slicePrimitiveToRawArray[T])
}

// encodeStringSparse extracts a string fill and patches and leaves
// child compression to the caller.
func encodeStringSparse(arr array.ArrayCore[string], children sparseChildBuilder[string]) (EncodedArray[string], error) {
	return buildSparseWithKey(arr, func(value string) string { return value }, children, sliceStringToRawArray)
}

// encodeStringSparseWithFill pins the empty string as the fill, sized for the
// non-null values of a null-dominated array.
func encodeStringSparseWithFill(arr array.ArrayCore[string], nullCount uint64, children sparseChildBuilder[string]) (EncodedArray[string], error) {
	return buildSparseWithFill(arr, func(value string) string { return value }, "", arr.Length()-min(nullCount, arr.Length()), children, sliceStringToRawArray)
}

func buildSparseWithKey[T Integer | Float | String, K comparable](arr array.ArrayCore[T], key func(T) K, children sparseChildBuilder[T], slice sliceBuilder[T]) (EncodedArray[T], error) {
	if children == nil {
		return nil, ErrBuilderRequired
	}
	n := arr.Length()
	if n == 0 {
		return children.BuildValues(sliceArrayCore[T](nil))
	}
	fill, mostFrequent := sparseMostFrequent(arr, key)
	if mostFrequent == n {
		return children.BuildValues(repeatedArrayCore[T]{length: n, value: fill})
	}
	return buildSparseWithFill(arr, key, fill, n-mostFrequent, children, slice)
}

func buildSparseWithFill[T Integer | Float | String, K comparable](arr array.ArrayCore[T], key func(T) K, fill T, patchCapacity uint64, children sparseChildBuilder[T], slice sliceBuilder[T]) (EncodedArray[T], error) {
	n := arr.Length()
	patchIndices := make([]uint64, 0, min(patchCapacity, n))
	patchValues := make([]T, 0, min(patchCapacity, n))
	for i := range n {
		value := arr.ValueAt(i)
		if key(value) != key(fill) {
			patchIndices = append(patchIndices, i)
			patchValues = append(patchValues, value)
		}
	}
	if len(patchValues) == 0 {
		return children.BuildValues(repeatedArrayCore[T]{length: n, value: fill})
	}
	fillCodec, err := children.BuildFill(sliceArrayCore[T]([]T{fill}))
	if err != nil {
		return nil, fmt.Errorf("codec: compress sparse fill: %w", err)
	}
	valueCodec, err := children.BuildValues(sliceArrayCore[T](patchValues))
	if err != nil {
		return nil, fmt.Errorf("codec: compress sparse values: %w", err)
	}
	patches, err := buildSparsePatches(n, fillCodec, patchIndices, valueCodec, children, slice)
	if err != nil {
		return nil, err
	}
	bitmapSize := n / 8
	if n&7 != 0 {
		bitmapSize++
	}
	bitmapLowerBound := uint64(headerSize) + bitmapSize + fillCodec.BinarySize() + valueCodec.BinarySize()
	if bitmapLowerBound >= patches.BinarySize() {
		return patches, nil
	}
	bitmapPatches, err := buildBitmapSparse(n, fillCodec, valueCodec, patchIndices, slice)
	if err != nil {
		return nil, err
	}
	if bitmapPatches.BinarySize() < patches.BinarySize() {
		return bitmapPatches, nil
	}
	return patches, nil
}

func sparseMostFrequent[T Integer | Float | String, K comparable](arr array.ArrayCore[T], key func(T) K) (T, uint64) {
	counts := make(map[K]uint64)
	var fill T
	var mostFrequent uint64
	for i := range arr.Length() {
		value := arr.ValueAt(i)
		valueKey := key(value)
		count := counts[valueKey] + 1
		counts[valueKey] = count
		if count > mostFrequent {
			fill, mostFrequent = value, count
		}
	}
	return fill, mostFrequent
}

func buildSparsePatches[T Integer | Float | String](length uint64, fill EncodedArray[T], indices []uint64, values EncodedArray[T], children sparseChildBuilder[T], slice sliceBuilder[T]) (EncodedArray[T], error) {
	switch {
	case length <= 1<<8:
		return buildSparsePatchesTyped[T, uint8](length, fill, indices, values, children.BuildUint8, slice)
	case length <= 1<<16:
		return buildSparsePatchesTyped[T, uint16](length, fill, indices, values, children.BuildUint16, slice)
	case length <= 1<<32:
		return buildSparsePatchesTyped[T, uint32](length, fill, indices, values, children.BuildUint32, slice)
	default:
		return buildSparsePatchesTyped[T, uint64](length, fill, indices, values, children.BuildUint64, slice)
	}
}

func buildSparsePatchesTyped[T Integer | Float | String, I UnsignedInteger](length uint64, fill EncodedArray[T], indices []uint64, values EncodedArray[T], buildIndices childBuilder[I], slice sliceBuilder[T]) (EncodedArray[T], error) {
	indexValues := make([]I, len(indices))
	for i, index := range indices {
		indexValues[i] = I(index)
	}
	indexCodec, err := buildIndices(array.NewPrimitivesUnsafe(indexValues))
	if err != nil {
		return nil, err
	}
	return &sparseArray[T, I]{denseRows: denseRows(length), fill: fill, indices: indexCodec, values: values, slice: slice}, nil
}
