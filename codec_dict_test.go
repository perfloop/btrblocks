package btrblocks

import "testing"

func TestDictCodecStringRoundTrip(t *testing.T) {
	data := []string{"east", "west", "east", "north", "west", "east", "south", "north"}

	codec, err := NewDictStringCodec(data, defaultDepth)
	if err != nil {
		t.Fatalf("NewDictStringCodec() returned error: %v", err)
	}

	assertCodecMetadata(t, codec, len(data), PTypeString, 2)
	assertCodecRoundTrip(t, codec, data)

	if got := codec.values.Length(); got != 4 {
		t.Fatalf("values.Length() = %d, want 4", got)
	}
	if got := codec.indices.Length(); got != uint64(len(data)) {
		t.Fatalf("indices.Length() = %d, want %d", got, len(data))
	}
}

func TestDictCodecLargeCorpus(t *testing.T) {
	data := makeLowCardinalityUint64Corpus(largeCorpusSize, 16)

	codec, err := NewDictIntegerCodec(data, defaultDepth)
	if err != nil {
		t.Fatalf("NewDictIntegerCodec() returned error: %v", err)
	}

	assertCodecMetadata(t, codec, len(data), PTypeUnsignedInteger, 2)
	assertCodecRoundTrip(t, codec, data)

	if got := codec.values.Length(); got != 16 {
		t.Fatalf("values.Length() = %d, want 16", got)
	}
	if got := codec.indices.Length(); got != uint64(len(data)) {
		t.Fatalf("indices.Length() = %d, want %d", got, len(data))
	}
}

func FuzzDictCodecRoundTrip(f *testing.F) {
	f.Add([]byte{0, 1, 0, 2, 0, 3})
	f.Add([]byte{9, 9, 9, 1, 1, 1})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 2048 {
			data = data[:2048]
		}

		values := make([]uint64, len(data))
		for i, b := range data {
			values[i] = uint64((int(b) + i) % 32)
		}

		codec, err := NewDictIntegerCodec(values, defaultDepth)
		if err != nil {
			t.Fatalf("NewDictIntegerCodec() returned error: %v", err)
		}

		assertCodecMetadata(t, codec, len(values), PTypeUnsignedInteger, 2)
		assertCodecRoundTrip(t, codec, values)
	})
}

func BenchmarkDictCodecBuildLarge(b *testing.B) {
	data := makeLowCardinalityUint64Corpus(largeCorpusSize, 16)
	benchmarkBuildLoop(b, "dict", func(values []uint64) (Codec[uint64], error) {
		return NewDictIntegerCodec(values, defaultDepth)
	}, data)
}

func BenchmarkDictCodecValueAtLarge(b *testing.B) {
	data := makeLowCardinalityUint64Corpus(largeCorpusSize, 16)
	codec, err := NewDictIntegerCodec(data, defaultDepth)
	if err != nil {
		b.Fatalf("NewDictIntegerCodec() returned error: %v", err)
	}
	benchmarkValueAtLoop(b, codec, len(data))
}
