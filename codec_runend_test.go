package btrblocks

import "testing"

func TestRunendCodecUint64RoundTrip(t *testing.T) {
	data := []uint64{5, 5, 5, 8, 8, 13, 13, 13}

	codec, err := NewRunendIntegerCodec(data, defaultDepth)
	if err != nil {
		t.Fatalf("NewRunendIntegerCodec() returned error: %v", err)
	}

	assertCodecMetadata(t, codec, len(data), PTypeUnsignedInteger, 2)
	assertCodecRoundTrip(t, codec, data)

	if got := codec.runs.Length(); got != 3 {
		t.Fatalf("runs.Length() = %d, want 3", got)
	}
	if got := codec.ends.Length(); got != 2 {
		t.Fatalf("ends.Length() = %d, want 2", got)
	}
}

func TestRunendCodecErrorsAndLargeCorpus(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		if _, err := NewRunendIntegerCodec([]uint64{}, defaultDepth); err != errDataEmpty {
			t.Fatalf("NewRunendIntegerCodec() error = %v, want %v", err, errDataEmpty)
		}
	})

	t.Run("large", func(t *testing.T) {
		data := makeRunUint64Corpus(largeCorpusSize, 4096)

		codec, err := NewRunendIntegerCodec(data, defaultDepth)
		if err != nil {
			t.Fatalf("NewRunendIntegerCodec() returned error: %v", err)
		}

		assertCodecMetadata(t, codec, len(data), PTypeUnsignedInteger, 2)
		assertCodecRoundTrip(t, codec, data)

		wantRuns := uint64((len(data) + 4096 - 1) / 4096)
		if got := codec.runs.Length(); got != wantRuns {
			t.Fatalf("runs.Length() = %d, want %d", got, wantRuns)
		}
		if got := codec.ends.Length(); got != wantRuns-1 {
			t.Fatalf("ends.Length() = %d, want %d", got, wantRuns-1)
		}
	})
}

func FuzzRunendCodecRoundTrip(f *testing.F) {
	f.Add([]byte{3, 2, 1})
	f.Add([]byte{8, 8, 8, 8})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 512 {
			data = data[:512]
		}

		values := make([]uint64, 0, len(data)*8)
		for i, b := range data {
			run := int(b%8) + 1
			value := uint64(i % 32)
			for j := 0; j < run; j++ {
				values = append(values, value)
			}
		}
		if len(values) == 0 {
			values = []uint64{0}
		}

		codec, err := NewRunendIntegerCodec(values, defaultDepth)
		if err != nil {
			t.Fatalf("NewRunendIntegerCodec() returned error: %v", err)
		}

		assertCodecMetadata(t, codec, len(values), PTypeUnsignedInteger, 2)
		assertCodecRoundTrip(t, codec, values)
	})
}

func BenchmarkRunendCodecBuildLarge(b *testing.B) {
	data := makeRunUint64Corpus(largeCorpusSize, 4096)
	benchmarkBuildLoop(b, "runend", func(values []uint64) (Codec[uint64], error) {
		return NewRunendIntegerCodec(values, defaultDepth)
	}, data)
}

func BenchmarkRunendCodecValueAtLarge(b *testing.B) {
	data := makeRunUint64Corpus(largeCorpusSize, 4096)
	codec, err := NewRunendIntegerCodec(data, defaultDepth)
	if err != nil {
		b.Fatalf("NewRunendIntegerCodec() returned error: %v", err)
	}
	benchmarkValueAtLoop(b, codec, len(data))
}
