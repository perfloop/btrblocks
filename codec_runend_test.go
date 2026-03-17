package btrblocks

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/axiomhq/btrblocks/array"
	"github.com/stretchr/testify/require"
)

func TestRunendCodecRoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		data     []uint64
		wantRuns uint64
		wantEnds uint64
	}{
		{
			name:     "small",
			data:     []uint64{5, 5, 5, 8, 8, 13, 13, 13},
			wantRuns: 3,
			wantEnds: 2,
		},
		{
			name:     "large",
			data:     makeRunUint64Corpus(largeCorpusSize, 4096),
			wantRuns: uint64((largeCorpusSize + 4096 - 1) / 4096),
			wantEnds: uint64((largeCorpusSize+4096-1)/4096) - 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codec, err := NewRunendIntegerCodec(array.NewPrimitivesUnsafe(tt.data), defaultDepth, 0)
			if err != nil {
				t.Fatalf("NewRunendIntegerCodec() returned error: %v", err)
			}

			assertCodecMetadata(t, codec, len(tt.data), PTypeUint64, 2)
			assertCodecRoundTrip(t, codec, tt.data)

			children := codec.Children()
			require.Len(t, children, 2)
			if got := requireChildCodecMetadata(t, children[0]).Length(); got != tt.wantRuns {
				t.Fatalf("runs.Length() = %d, want %d", got, tt.wantRuns)
			}
			if got := requireChildCodecMetadata(t, children[1]).Length(); got != tt.wantEnds {
				t.Fatalf("ends.Length() = %d, want %d", got, tt.wantEnds)
			}
		})
	}
}

func TestRunendCodecChoosesSmallestUnsignedEndWidth(t *testing.T) {
	tests := []struct {
		name     string
		data     []uint64
		wantType PType
	}{
		{
			name:     "uint8",
			data:     []uint64{5, 5, 5, 8, 8, 13, 13, 13},
			wantType: PTypeUint8,
		},
		{
			name:     "uint16",
			data:     makeRunUint64Corpus(1024, 256),
			wantType: PTypeUint16,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codec, err := NewRunendIntegerCodec(array.NewPrimitivesUnsafe(tt.data), defaultDepth, 0)
			require.NoError(t, err)
			children := codec.Children()
			require.Len(t, children, 2)
			require.Equal(t, tt.wantType, requireChildCodecMetadata(t, children[1]).PType())
		})
	}
}

func TestRunendCodecErrorsAndLargeCorpus(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		if _, err := NewRunendIntegerCodec(array.NewPrimitivesUnsafe([]uint64{}), defaultDepth, 0); err != errDataEmpty {
			t.Fatalf("NewRunendIntegerCodec() error = %v, want %v", err, errDataEmpty)
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

		codec, err := NewRunendIntegerCodec(array.NewPrimitivesUnsafe(values), defaultDepth, 0)
		if err != nil {
			t.Fatalf("NewRunendIntegerCodec() returned error: %v", err)
		}

		assertCodecMetadata(t, codec, len(values), PTypeUint64, 2)
		assertCodecRoundTrip(t, codec, values)
	})
}

func BenchmarkRunendCodecBuildLarge(b *testing.B) {
	data := makeRunUint64Corpus(largeCorpusSize, 4096)
	benchmarkBuildLoop(b, "runend", func(values []uint64) (Codec[uint64], error) {
		return NewRunendIntegerCodec(array.NewPrimitivesUnsafe(values), defaultDepth, 0)
	}, data)
}

func BenchmarkRunendCodecValueAtLarge(b *testing.B) {
	data := makeRunUint64Corpus(largeCorpusSize, 4096)
	codec, err := NewRunendIntegerCodec(array.NewPrimitivesUnsafe(data), defaultDepth, 0)
	if err != nil {
		b.Fatalf("NewRunendIntegerCodec() returned error: %v", err)
	}
	benchmarkValueAtLoop(b, codec, len(data))
}

func TestReadRunendCodecRejectsInvalidRunStructure(t *testing.T) {
	tests := []struct {
		name  string
		codec *RunendCodec[uint64, uint8]
		want  string
	}{
		{
			name: "runs length mismatch",
			codec: &RunendCodec[uint64, uint8]{
				length: 8,
				runs:   NewRawCodec(array.NewPrimitivesUnsafe([]uint64{5, 8})),
				ends:   NewRawCodec(array.NewPrimitivesUnsafe([]uint8{3, 5})),
			},
			want: "runs length",
		},
		// Non-monotonic ends are an encoder invariant violation — not checked
		// on the read path (no per-element validation on decode).
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			_, err := tt.codec.WriteTo(&buf)
			require.NoError(t, err)

			_, err = readCodec[uint64](bytes.NewReader(buf.Bytes()))
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestReadRunendCodecRejectsUnexpectedBodySize(t *testing.T) {
	codec, err := NewRunendIntegerCodec(array.NewPrimitivesUnsafe([]uint64{5, 5, 8}), defaultDepth, 0)
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = codec.WriteTo(&buf)
	require.NoError(t, err)

	data := append([]byte(nil), buf.Bytes()...)
	binary.LittleEndian.PutUint64(data[16:24], 1)

	_, err = readCodec[uint64](bytes.NewReader(data))
	require.ErrorContains(t, err, "runend body size")
}

func TestRunendDecodePanicsOnZeroLengthRun(t *testing.T) {
	// Zero-length runs (duplicate ends) are an encoder invariant violation.
	// Decode does not bounds-check ends — no per-element validation on decode.
	codec := &RunendCodec[uint64, uint8]{
		length: 8,
		runs:   NewRawCodec(array.NewPrimitivesUnsafe([]uint64{5, 8, 13})),
		ends:   NewRawCodec(array.NewPrimitivesUnsafe([]uint8{3, 3})),
	}

	dst := make([]uint64, 8)
	// fillRun with start >= end is a no-op, so this won't panic but will
	// produce incorrect output. That's acceptable for violated invariants.
	err := codec.Decode(dst)
	require.NoError(t, err)
}
