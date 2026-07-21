package compress

import (
	"testing"

	"github.com/axiomhq/btrblocks/codec"
)

func TestChildExclusions(t *testing.T) {
	tests := []struct {
		name      string
		parent    codec.CodecType
		child     uint8
		excluded  []codec.CodecType
		permitted []codec.CodecType
	}{
		{
			name:      "dictionary values are distinct",
			parent:    codec.CodecTypeDict,
			child:     0,
			excluded:  []codec.CodecType{codec.CodecTypeDict, codec.CodecTypeRunEnd, codec.CodecTypeSparse},
			permitted: []codec.CodecType{codec.CodecTypeALP, codec.CodecTypeFSST},
		},
		{
			name:      "dictionary codes are compact ordinals",
			parent:    codec.CodecTypeDict,
			child:     1,
			excluded:  []codec.CodecType{codec.CodecTypeDict, codec.CodecTypeFor, codec.CodecTypeSequence, codec.CodecTypeZigZag, codec.CodecTypeDelta},
			permitted: []codec.CodecType{codec.CodecTypeBitpack, codec.CodecTypeRunEnd, codec.CodecTypeSparse},
		},
		{
			name:      "run values can repeat non-adjacently",
			parent:    codec.CodecTypeRunEnd,
			child:     0,
			excluded:  []codec.CodecType{codec.CodecTypeRunEnd},
			permitted: []codec.CodecType{codec.CodecTypeDict},
		},
		{
			name:      "run ends are increasing and distinct",
			parent:    codec.CodecTypeRunEnd,
			child:     1,
			excluded:  []codec.CodecType{codec.CodecTypeRunEnd, codec.CodecTypeDict, codec.CodecTypeSparse},
			permitted: []codec.CodecType{codec.CodecTypeFor, codec.CodecTypeSequence, codec.CodecTypeDelta},
		},
		{
			name:      "zigzag preserves distribution",
			parent:    codec.CodecTypeZigZag,
			child:     0,
			excluded:  []codec.CodecType{codec.CodecTypeZigZag, codec.CodecTypeDict, codec.CodecTypeRunEnd, codec.CodecTypeSparse},
			permitted: []codec.CodecType{codec.CodecTypeBitpack, codec.CodecTypeFor},
		},
		{
			name:      "delta residuals use direct range codecs",
			parent:    codec.CodecTypeDelta,
			child:     0,
			excluded:  []codec.CodecType{codec.CodecTypeDelta, codec.CodecTypeDict, codec.CodecTypeRunEnd, codec.CodecTypeSparse, codec.CodecTypeSequence, codec.CodecTypeZigZag},
			permitted: []codec.CodecType{codec.CodecTypeConst, codec.CodecTypeBitpack, codec.CodecTypeFor},
		},
		{
			name:      "fsst lengths retain distribution codecs",
			parent:    codec.CodecTypeFSST,
			child:     1,
			excluded:  []codec.CodecType{codec.CodecTypeFSST},
			permitted: []codec.CodecType{codec.CodecTypeDict, codec.CodecTypeRunEnd, codec.CodecTypeSparse},
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
