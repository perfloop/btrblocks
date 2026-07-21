package compress

import (
	"testing"
)

func TestChildExclusions(t *testing.T) {
	tests := []struct {
		name      string
		parent    CodecType
		child     uint8
		excluded  []CodecType
		permitted []CodecType
	}{
		{
			name:      "dictionary values are distinct",
			parent:    CodecTypeDict,
			child:     0,
			excluded:  []CodecType{CodecTypeDict, CodecTypeRunEnd, CodecTypeSparse},
			permitted: []CodecType{CodecTypeALP, CodecTypeFSST},
		},
		{
			name:      "dictionary codes are compact ordinals",
			parent:    CodecTypeDict,
			child:     1,
			excluded:  []CodecType{CodecTypeDict, CodecTypeFor, CodecTypeSequence, CodecTypeZigZag, CodecTypeDelta},
			permitted: []CodecType{CodecTypeBitpack, CodecTypeRunEnd, CodecTypeSparse},
		},
		{
			name:      "run values can repeat non-adjacently",
			parent:    CodecTypeRunEnd,
			child:     0,
			excluded:  []CodecType{CodecTypeRunEnd},
			permitted: []CodecType{CodecTypeDict},
		},
		{
			name:      "run ends are increasing and distinct",
			parent:    CodecTypeRunEnd,
			child:     1,
			excluded:  []CodecType{CodecTypeRunEnd, CodecTypeDict, CodecTypeSparse},
			permitted: []CodecType{CodecTypeFor, CodecTypeSequence, CodecTypeDelta},
		},
		{
			name:      "zigzag preserves distribution",
			parent:    CodecTypeZigZag,
			child:     0,
			excluded:  []CodecType{CodecTypeZigZag, CodecTypeDict, CodecTypeRunEnd, CodecTypeSparse},
			permitted: []CodecType{CodecTypeBitpack, CodecTypeFor},
		},
		{
			name:      "delta residuals use direct range codecs",
			parent:    CodecTypeDelta,
			child:     0,
			excluded:  []CodecType{CodecTypeDelta, CodecTypeDict, CodecTypeRunEnd, CodecTypeSparse, CodecTypeSequence, CodecTypeZigZag},
			permitted: []CodecType{CodecTypeConst, CodecTypeBitpack, CodecTypeFor},
		},
		{
			name:      "fsst lengths retain distribution codecs",
			parent:    CodecTypeFSST,
			child:     1,
			excluded:  []CodecType{CodecTypeFSST},
			permitted: []CodecType{CodecTypeDict, CodecTypeRunEnd, CodecTypeSparse},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			exclusions := childExclusions(test.parent, test.child)
			for _, kind := range test.excluded {
				if !exclusions.Has(kind) {
					t.Errorf("%s must be excluded", kind)
				}
			}
			for _, kind := range test.permitted {
				if exclusions.Has(kind) {
					t.Errorf("%s must be permitted", kind)
				}
			}
		})
	}
}
