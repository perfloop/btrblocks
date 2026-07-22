package codec

import (
	"fmt"
	"io"
	"iter"
	"math"
	"math/bits"
	"sync"
	"sync/atomic"

	"github.com/kamstrup/intmap"

	"github.com/axiomhq/btrblocks/array"
)

// dictArray stores unique values plus an ordinal child that indexes into them.
type dictArray[V Integer | Float | String, I UnsignedInteger] struct {
	encodedNode
	decodeLimit
	values  EncodedArray[V]
	indices EncodedArray[I]
	visits  atomic.Uint32
	bsiOnce sync.Once
	bsi     *ordinalBSI
}

type ordinalBSI struct {
	planes [][]uint64
	length uint64
}

func (d *dictArray[V, I]) CodecType() CodecType { return CodecTypeDict }
func (d *dictArray[V, I]) Length() uint64       { return d.indices.Length() }
func (d *dictArray[V, I]) IsValid(offset uint64) bool {
	return allValidAt(d.Length(), offset)
}
func (d *dictArray[V, I]) NullCount() uint64 { return 0 }
func (d *dictArray[V, I]) PType() PType      { return d.values.PType() }
func (d *dictArray[V, I]) DecodedBytes() (uint64, error) {
	// DecompressInto bulk-decodes both children before gathering, so all three
	// buffers are live at once.
	var f decodeFootprint
	f.add(decodedBytesFor(d.Length(), d.PType()))
	f.add(d.values.DecodedBytes())
	f.add(d.indices.DecodedBytes())
	return f.result()
}

func (d *dictArray[V, I]) NumDictionaryValues() uint64 { return d.values.Length() }

func (d *dictArray[V, I]) DecompressDictionaryInto(dst []V) error {
	if err := d.values.DecompressInto(dst); err != nil {
		return fmt.Errorf("codec: decompress dictionary values: %w", err)
	}
	return nil
}

func (d *dictArray[V, I]) VisitMatchingOrdinals(matches []bool, visit func(offset uint64)) error {
	if uint64(len(matches)) < d.values.Length() {
		return fmt.Errorf("codec: dictionary matches length %d, want at least %d", len(matches), d.values.Length())
	}
	matchCount := 0
	for _, match := range matches[:d.values.Length()] {
		if match {
			matchCount++
		}
	}
	if (matchCount == 1 || matchCount+1 == int(d.values.Length())) && d.visits.Add(1) > 1 {
		d.bsiOnce.Do(func() {
			d.bsi = buildOrdinalBSI(d.indices, d.values.Length())
		})
		if d.bsi != nil {
			d.bsi.visit(matches[:d.values.Length()], visit)
			return nil
		}
	}
	handled, err := visitDirectMatchingOrdinals(d.indices, d.values.Length(), matches, visit)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	ordinals := make([]I, d.indices.Length())
	if err := d.indices.DecompressInto(ordinals); err != nil {
		return fmt.Errorf("codec: decompress dictionary ordinals: %w", err)
	}
	for i, ordinal := range ordinals {
		if uint64(ordinal) >= d.values.Length() {
			return fmt.Errorf("codec: dict ordinal %d at position %d >= values length %d", ordinal, i, d.values.Length())
		}
		if matches[ordinal] {
			visit(uint64(i))
		}
	}
	return nil
}

func buildOrdinalBSI[I UnsignedInteger](encoded EncodedArray[I], numValues uint64) *ordinalBSI {
	if numValues == 0 {
		return nil
	}
	bitWidth := bits.Len64(numValues - 1)
	if bitWidth == 0 {
		return &ordinalBSI{length: encoded.Length()}
	}
	wordCount := (encoded.Length() + 63) / 64
	result := &ordinalBSI{planes: make([][]uint64, bitWidth), length: encoded.Length()}
	for bit := range result.planes {
		result.planes[bit] = make([]uint64, wordCount)
	}
	valid := true
	if !visitDirectOrdinals(encoded, func(offset, ordinal uint64) {
		if ordinal >= numValues {
			valid = false
			return
		}
		for value := ordinal; value != 0; value &= value - 1 {
			bit := bits.TrailingZeros64(value)
			result.planes[bit][offset/64] |= uint64(1) << (offset & 63)
		}
	}) || !valid {
		return nil
	}
	return result
}

func (b *ordinalBSI) visit(matches []bool, visit func(offset uint64)) {
	matchingOrdinal := uint64(0)
	matchCount := 0
	for ordinal, match := range matches {
		if match {
			matchingOrdinal = uint64(ordinal)
			matchCount++
		}
	}
	invert := matchCount != 1
	if invert {
		for ordinal, match := range matches {
			if !match {
				matchingOrdinal = uint64(ordinal)
				break
			}
		}
	}
	wordCount := (b.length + 63) / 64
	for wordIndex := range wordCount {
		word := ^uint64(0)
		for bit, plane := range b.planes {
			if matchingOrdinal&(uint64(1)<<bit) != 0 {
				word &= plane[wordIndex]
			} else {
				word &^= plane[wordIndex]
			}
		}
		if invert {
			word = ^word
		}
		if wordIndex+1 == wordCount && b.length&63 != 0 {
			word &= (uint64(1) << (b.length & 63)) - 1
		}
		for word != 0 {
			bit := bits.TrailingZeros64(word)
			visit(wordIndex*64 + uint64(bit))
			word &= word - 1
		}
	}
}

// visitDirectMatchingOrdinals handles the codec tree produced for dictionary
// ordinals without materializing an N-element ordinal slice. Dictionary codes
// are non-negative and normally encode as FoR(bitpack), with raw and constant
// covering the small fallback cases.
func visitDirectMatchingOrdinals[I UnsignedInteger](encoded EncodedArray[I], numValues uint64, matches []bool, visit func(offset uint64)) (bool, error) {
	var invalidOrdinal uint64
	var invalidOffset uint64
	valid := true
	handled := visitDirectOrdinals(encoded, func(offset, ordinal uint64) {
		if ordinal >= numValues {
			if valid {
				invalidOrdinal = ordinal
				invalidOffset = offset
				valid = false
			}
			return
		}
		if matches[ordinal] {
			visit(offset)
		}
	})
	if handled && !valid {
		return true, fmt.Errorf("codec: dict ordinal %d at position %d >= values length %d", invalidOrdinal, invalidOffset, numValues)
	}
	return handled, nil
}

func visitDirectOrdinals[I UnsignedInteger](encoded EncodedArray[I], visit func(offset, ordinal uint64)) bool {
	switch values := encoded.(type) {
	case *forArray[I]:
		packed, ok := values.child.(*bitPackedArray[I, uint64])
		if !ok || packed.patches != nil {
			return false
		}
		visitBitpackedOrdinals(packed, uint64(values.min), visit)
		return true
	case *bitPackedArray[I, uint64]:
		if values.patches != nil {
			return false
		}
		visitBitpackedOrdinals(values, 0, visit)
		return true
	case *constArray[I]:
		ordinal := uint64(values.body.ValueAt(0))
		for offset := range values.Length() {
			visit(offset, ordinal)
		}
		return true
	case *rawArray[I]:
		for offset := range values.Length() {
			ordinal := uint64(values.ValueAt(offset))
			visit(offset, ordinal)
		}
		return true
	case *deltaArray[I]:
		current := values.base
		visit(0, uint64(current))
		return visitDirectOrdinals(values.child, func(offset, delta uint64) {
			current += I(delta)
			visit(offset+1, uint64(current))
		})
	case *runEndArray[I, uint8]:
		return visitRunEndOrdinals(values, visit)
	case *runEndArray[I, uint16]:
		return visitRunEndOrdinals(values, visit)
	case *runEndArray[I, uint32]:
		return visitRunEndOrdinals(values, visit)
	case *runEndArray[I, uint64]:
		return visitRunEndOrdinals(values, visit)
	default:
		return false
	}
}

func visitRunEndOrdinals[V UnsignedInteger, I UnsignedInteger](values *runEndArray[V, I], visit func(offset, ordinal uint64)) bool {
	runs, err := decompress(values.runs, values.maxDecodedBytes())
	if err != nil {
		return false
	}
	ends, err := decompress(values.ends, values.maxDecodedBytes())
	if err != nil {
		return false
	}
	start := uint64(0)
	for run, rawEnd := range ends {
		end := uint64(rawEnd)
		for offset := start; offset < end; offset++ {
			visit(offset, uint64(runs[run]))
		}
		start = end
	}
	for offset := start; offset < values.Length(); offset++ {
		visit(offset, uint64(runs[len(runs)-1]))
	}
	return true
}

func visitBitpackedOrdinals[I UnsignedInteger](values *bitPackedArray[I, uint64], base uint64, visit func(offset, ordinal uint64)) {
	for offset := range values.Length() {
		ordinal := base
		if values.bitWidth != 0 {
			ordinal += unpackUnsigned(values.buf, offset*uint64(values.bitWidth), values.bitWidth)
		}
		visit(offset, ordinal)
	}
}

func (d *dictArray[V, I]) BinarySize() uint64 {
	return uint64(headerSize) + d.values.BinarySize() + d.indices.BinarySize()
}

func (d *dictArray[V, I]) MarshalBinary() ([]byte, error) { return marshalBinary(d) }

func (d *dictArray[V, I]) ValueAt(offset uint64) V {
	// A corrupt ordinal panics via the values child's bounds check, matching this
	// layer's ValueAt contract (panics on out-of-range access) and Arrow's
	// Value(i). We don't scan every ordinal at load; DecompressInto and
	// VisitMatchingOrdinals range-check and return an error for callers that must
	// not panic on an untrusted page.
	return d.values.ValueAt(uint64(d.indices.ValueAt(offset)))
}

// DecompressInto decodes the dictionary into dst. It bulk-decodes both the
// values and indices children into intermediate slices, then performs a single
// gather pass. Per-element ValueAt was benchmarked and is 3.3x slower at 10K
// elements because it loses the batch-optimized unpackBatchTyped fast path in
// the bitpacked indices child — each call traverses the codec tree individually
// instead of one bulk decode. The intermediate allocation (~N * index_width
// bytes) is the cost of keeping the fast path.
func (d *dictArray[V, I]) DecompressInto(dst []V) error {
	if err := checkDstLen(dst, d.indices.Length()); err != nil {
		return err
	}
	values, err := decompress(d.values, d.maxDecodedBytes())
	if err != nil {
		return err
	}
	indices, err := decompress(d.indices, d.maxDecodedBytes())
	if err != nil {
		return err
	}
	nValues := len(values)
	for i, idx := range indices {
		if uint64(idx) >= uint64(nValues) {
			return fmt.Errorf("codec: dict ordinal %d at position %d >= values length %d", idx, i, nValues)
		}
		dst[i] = values[idx]
	}
	return nil
}

func (d *dictArray[V, I]) Slice(start, end uint64) (EncodedArray[V], error) {
	indices, err := d.indices.Slice(start, end)
	if err != nil {
		return nil, err
	}
	return &dictArray[V, I]{decodeLimit: d.decodeLimit, values: d.values, indices: indices}, nil
}

func (d *dictArray[V, I]) WriteTo(w io.Writer) (int64, error) {
	sum := writeSum{w: w}
	if err := sum.writeTo(codecHeader{
		Version:  versionNumber,
		Type:     CodecTypeDict,
		ElemType: d.values.PType(),
		Length:   d.indices.Length(),
		NumBytes: 0,
	}); err != nil {
		return sum.n, err
	}
	if err := sum.writeTo(d.values); err != nil {
		return sum.n, err
	}
	if err := sum.writeTo(d.indices); err != nil {
		return sum.n, err
	}
	return sum.n, nil
}

func readDictArray[V Integer | Float | String](br *array.BufReader, h codecHeader, opts *readOptions, readValues encodedReader[V]) (EncodedArray[V], error) {
	if h.NumBytes != 0 {
		return nil, fmt.Errorf("codec: dict body size = %d, want 0", h.NumBytes)
	}
	values, err := readValues(br, opts)
	if err != nil {
		return nil, fmt.Errorf("codec: dict values: %w", err)
	}
	if err := requireNonNullable(values, "dictionary values"); err != nil {
		return nil, err
	}
	childHeader, err := readHeader(br)
	if err != nil {
		return nil, fmt.Errorf("codec: dict ordinals header: %w", err)
	}
	switch childHeader.ElemType {
	case PTypeUint8:
		return readDictOrdinals[V, uint8](br, h, childHeader, opts, values)
	case PTypeUint16:
		return readDictOrdinals[V, uint16](br, h, childHeader, opts, values)
	case PTypeUint32:
		return readDictOrdinals[V, uint32](br, h, childHeader, opts, values)
	case PTypeUint64:
		return readDictOrdinals[V, uint64](br, h, childHeader, opts, values)
	default:
		return nil, fmt.Errorf("codec: dict ordinal child type = %v, want unsigned integer", childHeader.ElemType)
	}
}

func readDictOrdinals[V Integer | Float | String, I UnsignedInteger](br *array.BufReader, h codecHeader, childHeader codecHeader, opts *readOptions, values EncodedArray[V]) (EncodedArray[V], error) {
	indices, err := readUnsignedEncodedArrayWithHeader[I](br, childHeader, opts)
	if err != nil {
		return nil, fmt.Errorf("codec: dict ordinals: %w", err)
	}
	if err := requireNonNullable(indices, "dictionary ordinals"); err != nil {
		return nil, err
	}
	if indices.Length() != h.Length {
		return nil, fmt.Errorf("codec: dict length = %d, want %d", indices.Length(), h.Length)
	}
	ordinals, err := scanChild(indices, opts)
	if err != nil {
		return nil, fmt.Errorf("codec: dict ordinal scan: %w", err)
	}
	valuesLength := values.Length()
	for i, ordinal := range ordinals {
		if uint64(ordinal) >= valuesLength {
			return nil, fmt.Errorf("codec: dict ordinal %d at position %d exceeds values length %d", ordinal, i, valuesLength)
		}
	}
	return &dictArray[V, I]{decodeLimit: opts.decodeLimit(), values: values, indices: indices}, nil
}

// distinctLookup abstracts the key->ordinal dictionary buildDictWithKey and
// buildDictCodes read from. *intmap.Map[K, uint64] (int/float-bits keys)
// satisfies it natively; mapLookup adapts a plain Go map (string keys,
// which intmap.IntKey doesn't cover). Neither backend is ever converted
// into the other: buildIntegerDictFromDistinct/buildFloatDictFromDistinct
// build an intmap once and use it all the way through, including the
// per-row Get in buildDictCodes.
type distinctLookup[K comparable] interface {
	Get(K) (uint64, bool)
	Len() int
	All() iter.Seq2[K, uint64]
}

// mapLookup adapts a plain Go map to distinctLookup, for the string dict
// path (intmap.IntKey has no string case).
type mapLookup[K comparable] map[K]uint64

func (m mapLookup[K]) Get(k K) (uint64, bool) { v, ok := m[k]; return v, ok }
func (m mapLookup[K]) Len() int               { return len(m) }
func (m mapLookup[K]) All() iter.Seq2[K, uint64] {
	return func(yield func(K, uint64) bool) {
		for k, v := range m {
			if !yield(k, v) {
				return
			}
		}
	}
}

func buildIntegerDictFromDistinctWithChildren[T Integer](arr array.ArrayCore[T], distinct distinctLookup[T], children DictionaryChildBuilder[T], budget buildBudget) (EncodedArray[T], error) {
	if children == nil {
		return nil, ErrBuilderRequired
	}

	n := arr.Length()
	if distinct == nil || distinct.Len() == 0 {
		// intmap.Map beats a Go map[T]uint64 here (~2.5x faster, far fewer
		// allocs: open addressing into one backing array vs Go's per-bucket
		// allocation) for this "insert if absent, keep the ordinal" pattern.
		if err := checkBuildMapEntries[T](budget, 8, "integer dictionary map"); err != nil {
			return nil, err
		}
		im := intmap.New[T, uint64](8)
		entryLimit := buildMapEntryLimit[T](budget)
		for i := range n {
			v := arr.ValueAt(i)
			if uint64(im.Len()) >= entryLimit {
				if _, exists := im.Get(v); !exists {
					return nil, fmt.Errorf("%w: integer dictionary map exceeds %d bytes", ErrMaterializationLimit, budget.maxBytes)
				}
				continue
			}
			im.PutIfNotExists(v, uint64(im.Len()))
		}
		distinct = im
	}

	if err := checkBuildMapEntries[T](budget, uint64(distinct.Len()), "integer dictionary map"); err != nil {
		return nil, err
	}
	values, err := makeBuildSlice[T](budget, uint64(distinct.Len()), uint64(distinct.Len()), "dictionary values")
	if err != nil {
		return nil, err
	}
	for v, ord := range distinct.All() {
		values[ord] = v
	}

	valuesCodec, err := children.BuildValues(sliceArrayCore[T](values))
	valuesCodec, err = adoptChild(uint64(len(values)), valuesCodec, err)
	if err != nil {
		return nil, fmt.Errorf("codec: compress dictionary values: %w", err)
	}
	return buildDictWithKey(arr, distinct, identityKey[T], valuesCodec, children, budget)
}

func identityKey[T comparable](value T) T { return value }

// buildDictWithKey picks the narrowest ordinal width for the dictionary size —
// max code is len(distinct)-1 — and builds the codes child. The key function
// maps a value to its distinct-map key (identity for integers and strings,
// floatBits for floats).
func buildDictWithKey[T Integer | Float | String, K comparable](arr array.ArrayCore[T], distinct distinctLookup[K], key func(T) K, valuesCodec EncodedArray[T], children DictionaryChildBuilder[T], budget buildBudget) (EncodedArray[T], error) {
	switch numDistinct := distinct.Len(); {
	case numDistinct <= 1<<8:
		return buildDictCodes(arr, distinct, key, valuesCodec, children.BuildUint8, budget)
	case numDistinct <= 1<<16:
		return buildDictCodes(arr, distinct, key, valuesCodec, children.BuildUint16, budget)
	case uint64(numDistinct) <= uint64(1)<<32:
		return buildDictCodes(arr, distinct, key, valuesCodec, children.BuildUint32, budget)
	default:
		return buildDictCodes(arr, distinct, key, valuesCodec, children.BuildUint64, budget)
	}
}

// buildDictCodes builds the codes array as []I directly from the distinct map.
func buildDictCodes[T Integer | Float | String, K comparable, I UnsignedInteger](arr array.ArrayCore[T], distinct distinctLookup[K], key func(T) K, valuesCodec EncodedArray[T], buildCodes ChildBuilder[I], budget buildBudget) (EncodedArray[T], error) {
	n := arr.Length()
	codes, err := makeBuildSlice[I](budget, n, n, "dictionary codes")
	if err != nil {
		return nil, err
	}
	for i := range n {
		ord, _ := distinct.Get(key(arr.ValueAt(i)))
		codes[i] = I(ord)
	}
	codesCodec, err := buildCodes(array.NewPrimitivesUnsafe(codes))
	codesCodec, err = adoptChild(n, codesCodec, err)
	if err != nil {
		return nil, fmt.Errorf("codec: compress dictionary codes: %w", err)
	}
	return &dictArray[T, I]{values: valuesCodec, indices: codesCodec}, nil
}

func buildFloatDictFromDistinctCapacityWithChildren[T Float](arr array.ArrayCore[T], distinct distinctLookup[uint64], expectedDistinct uint64, fromBits func(uint64) T, children DictionaryChildBuilder[T], budget buildBudget) (EncodedArray[T], error) {
	if children == nil {
		return nil, ErrBuilderRequired
	}

	n := arr.Length()
	if distinct == nil || distinct.Len() == 0 {
		capacity := 8
		if expectedDistinct > uint64(capacity) {
			capacity = int(min(expectedDistinct, n))
		}
		if err := checkBuildMapEntries[uint64](budget, uint64(capacity), "float dictionary map"); err != nil {
			return nil, err
		}
		im := intmap.New[uint64, uint64](capacity)
		entryLimit := buildMapEntryLimit[uint64](budget)
		for i := range n {
			key := array.FloatBits(arr.ValueAt(i))
			if uint64(im.Len()) >= entryLimit {
				if _, exists := im.Get(key); !exists {
					return nil, fmt.Errorf("%w: float dictionary map exceeds %d bytes", ErrMaterializationLimit, budget.maxBytes)
				}
				continue
			}
			im.PutIfNotExists(key, uint64(im.Len()))
		}
		distinct = im
	}

	if err := checkBuildMapEntries[uint64](budget, uint64(distinct.Len()), "float dictionary map"); err != nil {
		return nil, err
	}
	values, err := makeBuildSlice[T](budget, uint64(distinct.Len()), uint64(distinct.Len()), "float dictionary values")
	if err != nil {
		return nil, err
	}
	for bits, ord := range distinct.All() {
		values[ord] = fromBits(bits)
	}

	valuesCodec, err := children.BuildValues(sliceArrayCore[T](values))
	valuesCodec, err = adoptChild(uint64(len(values)), valuesCodec, err)
	if err != nil {
		return nil, fmt.Errorf("codec: compress float dictionary values: %w", err)
	}
	return buildDictWithKey(arr, distinct, array.FloatBits[T], valuesCodec, children, budget)
}

// encodeIntegerDict builds an integer dictionary while leaving
// value and ordinal child selection to the caller.
func encodeIntegerDict[T Integer](arr array.ArrayCore[T], distinct map[T]uint64, children DictionaryChildBuilder[T], budget buildBudget) (EncodedArray[T], error) {
	return buildIntegerDictFromDistinctWithChildren(arr, mapLookup[T](distinct), children, budget)
}

// BuildFloatDictWithChildren builds a float dictionary with a cardinality hint
// while leaving value and ordinal child selection to the caller.
func encodeFloat32Dict(arr array.ArrayCore[float32], distinct map[uint64]uint64, expectedDistinct uint64, children DictionaryChildBuilder[float32], budget buildBudget) (EncodedArray[float32], error) {
	return buildFloatDictFromDistinctCapacityWithChildren(arr, mapLookup[uint64](distinct), expectedDistinct, func(bits uint64) float32 {
		return math.Float32frombits(uint32(bits))
	}, children, budget)
}

// encodeFloat64Dict builds a float64 dictionary and its children.
func encodeFloat64Dict(arr array.ArrayCore[float64], distinct map[uint64]uint64, expectedDistinct uint64, children DictionaryChildBuilder[float64], budget buildBudget) (EncodedArray[float64], error) {
	return buildFloatDictFromDistinctCapacityWithChildren(arr, mapLookup[uint64](distinct), expectedDistinct, math.Float64frombits, children, budget)
}

func buildStringDictWithChildren[T String](arr array.ArrayCore[T], children DictionaryChildBuilder[T], budget buildBudget) (EncodedArray[T], error) {
	if children == nil {
		return nil, ErrBuilderRequired
	}
	n := arr.Length()
	dict := make(mapLookup[T])
	values, err := makeBuildSlice[T](budget, 0, n, "string dictionary values")
	if err != nil {
		return nil, err
	}
	for i := range n {
		value := arr.ValueAt(i)
		if _, ok := dict[value]; !ok {
			if err := checkBuildMapEntries[T](budget, uint64(len(dict))+1, "string dictionary map"); err != nil {
				return nil, err
			}
			dict[value] = uint64(len(values))
			values = append(values, value)
		}
	}

	valuesCodec, err := children.BuildValues(sliceArrayCore[T](values))
	valuesCodec, err = adoptChild(uint64(len(values)), valuesCodec, err)
	if err != nil {
		return nil, fmt.Errorf("codec: compress string dictionary values: %w", err)
	}
	return buildDictWithKey(arr, dict, identityKey[T], valuesCodec, children, budget)
}

// encodeStringDict builds a string dictionary while leaving value
// and ordinal child selection to the caller.
func encodeStringDict[T String](arr array.ArrayCore[T], children DictionaryChildBuilder[T], budget buildBudget) (EncodedArray[T], error) {
	return buildStringDictWithChildren(arr, children, budget)
}
