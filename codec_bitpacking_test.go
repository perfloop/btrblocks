package btrblocks

import "testing"

func TestBitpackingCodecUint8CrossByteRoundTrip(t *testing.T) {
	data := []uint8{0, 1, 2, 3, 4, 5, 6, 7}
	codec := NewBitpackingCodec(data)

	assertCodecMetadata(t, codec, len(data), PTypeUint8, 0)
	assertCodecRoundTrip(t, codec, data)

	if got := codec.bitWidth; got != 3 {
		t.Fatalf("bitWidth = %d, want 3", got)
	}
	if got := len(codec.buf); got != 3 {
		t.Fatalf("len(buf) = %d, want 3", got)
	}
}

func TestBitpackingCodecZeroAndWideValues(t *testing.T) {
	t.Run("all zero", func(t *testing.T) {
		data := makeConstantCorpus(256, uint16(0))
		codec := NewBitpackingCodec(data)

		assertCodecMetadata(t, codec, len(data), PTypeUint16, 0)
		assertCodecRoundTrip(t, codec, data)

		if got := codec.bitWidth; got != 0 {
			t.Fatalf("bitWidth = %d, want 0", got)
		}
		if got := len(codec.buf); got != 0 {
			t.Fatalf("len(buf) = %d, want 0", got)
		}
	})

	t.Run("max uint64", func(t *testing.T) {
		data := []uint64{0, ^uint64(0), 1 << 63, 17}
		codec := NewBitpackingCodec(data)

		assertCodecMetadata(t, codec, len(data), PTypeUint64, 0)
		assertCodecRoundTrip(t, codec, data)

		if got := codec.bitWidth; got != 64 {
			t.Fatalf("bitWidth = %d, want 64", got)
		}
		if got := len(codec.buf); got != 32 {
			t.Fatalf("len(buf) = %d, want 32", got)
		}
	})
}

func TestBitpackingCodecLargeCorpus(t *testing.T) {
	data := make([]uint32, largeCorpusSize)
	for i := range data {
		data[i] = uint32(i % 31)
	}

	codec := NewBitpackingCodec(data)
	assertCodecMetadata(t, codec, len(data), PTypeUint32, 0)
	assertCodecRoundTrip(t, codec, data)

	if got := codec.bitWidth; got != 5 {
		t.Fatalf("bitWidth = %d, want 5", got)
	}
	if got := len(codec.buf); got != packedByteSize(uint64(len(data)), codec.bitWidth) {
		t.Fatalf("len(buf) = %d, want %d", got, packedByteSize(uint64(len(data)), codec.bitWidth))
	}
}

func FuzzBitpackingCodecRoundTrip(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3})
	f.Add([]byte{31, 0, 31, 0, 31})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			data = data[:4096]
		}

		values := make([]uint16, len(data))
		for i, b := range data {
			values[i] = uint16(b) | uint16(i&3)<<8
		}

		codec := NewBitpackingCodec(values)
		assertCodecMetadata(t, codec, len(values), PTypeUint16, 0)
		assertCodecRoundTrip(t, codec, values)
	})
}

func BenchmarkBitpackingCodecBuildLarge(b *testing.B) {
	data := make([]uint32, largeCorpusSize)
	for i := range data {
		data[i] = uint32(i % 31)
	}

	benchmarkBuildLoop(b, "bitpacking", func(values []uint32) (Codec[uint32], error) {
		return NewBitpackingCodec(values), nil
	}, data)
}

func BenchmarkBitpackingCodecValueAtLarge(b *testing.B) {
	data := make([]uint32, largeCorpusSize)
	for i := range data {
		data[i] = uint32(i % 31)
	}

	codec := NewBitpackingCodec(data)
	benchmarkValueAtLoop(b, codec, len(data))
}
