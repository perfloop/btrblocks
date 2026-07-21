package array

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPTypeStringAndClassifiers(t *testing.T) {
	tests := []struct {
		pType       PType
		wantString  string
		wantInteger bool
		wantFloat   bool
		wantStringy bool
		wantBytes   int
	}{
		{pType: PTypeUnknown, wantString: "unknown"},
		{pType: PTypeInt8, wantString: "int8", wantInteger: true, wantBytes: 1},
		{pType: PTypeUint64, wantString: "uint64", wantInteger: true, wantBytes: 8},
		{pType: PTypeFloat32, wantString: "float32", wantFloat: true, wantBytes: 4},
		{pType: PTypeString, wantString: "string", wantStringy: true},
	}

	for _, tt := range tests {
		require.Equal(t, tt.wantString, tt.pType.String())
		require.Equal(t, tt.wantInteger, tt.pType.IsInteger())
		require.Equal(t, tt.wantFloat, tt.pType.IsFloat())
		require.Equal(t, tt.wantStringy, tt.pType.IsString())
		require.Equal(t, tt.wantInteger || tt.wantFloat || tt.wantStringy, tt.pType.IsPrimitive())
		require.Equal(t, tt.wantBytes, tt.pType.ByteWidth())
	}
}

func BenchmarkPTypeString(b *testing.B) {
	p := PTypeInt32
	b.ResetTimer()
	for range b.N {
		_ = p.String()
	}
}

func BenchmarkPTypeByteWidth(b *testing.B) {
	p := PTypeUint64
	b.ResetTimer()
	for range b.N {
		_ = p.ByteWidth()
	}
}

func FuzzPTypeString(f *testing.F) {
	for _, p := range []PType{PTypeUnknown, PTypeInt8, PTypeUint64, PTypeFloat32, PTypeString, 255} {
		f.Add(uint8(p))
	}
	f.Fuzz(func(t *testing.T, raw uint8) {
		p := PType(raw)
		_ = p.String()
		_ = p.IsInteger()
		_ = p.IsFloat()
		_ = p.IsString()
		_ = p.IsPrimitive()
		_ = p.ByteWidth()
		// Must not panic for any uint8 value.
	})
}
