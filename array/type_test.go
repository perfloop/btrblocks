package array

import "testing"

func TestPTypeStringAndClassifiers(t *testing.T) {
	tests := []struct {
		pType       PType
		wantString  string
		wantInteger bool
		wantFloat   bool
		wantStringy bool
	}{
		{pType: PTypeUnknown, wantString: "unknown"},
		{pType: PTypeInt8, wantString: "int8", wantInteger: true},
		{pType: PTypeUint64, wantString: "uint64", wantInteger: true},
		{pType: PTypeFloat32, wantString: "float32", wantFloat: true},
		{pType: PTypeString, wantString: "string", wantStringy: true},
	}

	for _, tt := range tests {
		if got := tt.pType.String(); got != tt.wantString {
			t.Fatalf("%v.String() = %q, want %q", tt.pType, got, tt.wantString)
		}
		if got := tt.pType.IsInteger(); got != tt.wantInteger {
			t.Fatalf("%v.IsInteger() = %t, want %t", tt.pType, got, tt.wantInteger)
		}
		if got := tt.pType.IsFloat(); got != tt.wantFloat {
			t.Fatalf("%v.IsFloat() = %t, want %t", tt.pType, got, tt.wantFloat)
		}
		if got := tt.pType.IsString(); got != tt.wantStringy {
			t.Fatalf("%v.IsString() = %t, want %t", tt.pType, got, tt.wantStringy)
		}
		if got := tt.pType.IsPrimitive(); got != (tt.wantInteger || tt.wantFloat || tt.wantStringy) {
			t.Fatalf("%v.IsPrimitive() = %t", tt.pType, got)
		}
	}
}

func TestPTypeForType(t *testing.T) {
	assertPTypeForType[int8](t, PTypeInt8)
	assertPTypeForType[int16](t, PTypeInt16)
	assertPTypeForType[int32](t, PTypeInt32)
	assertPTypeForType[int64](t, PTypeInt64)
	assertPTypeForType[uint8](t, PTypeUint8)
	assertPTypeForType[uint16](t, PTypeUint16)
	assertPTypeForType[uint32](t, PTypeUint32)
	assertPTypeForType[uint64](t, PTypeUint64)
	assertPTypeForType[float32](t, PTypeFloat32)
	assertPTypeForType[float64](t, PTypeFloat64)
	assertPTypeForType[string](t, PTypeString)
}

func assertPTypeForType[T Integer | Float | String](t *testing.T, want PType) {
	t.Helper()

	if got := pTypeForType[T](); got != want {
		t.Fatalf("pTypeForType() = %v, want %v", got, want)
	}
}
