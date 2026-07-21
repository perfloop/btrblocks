package compress

import "github.com/axiomhq/btrblocks/codec"

func childExclusions(parent codec.CodecType, childIndex uint8) excludeSet {
	self := excludeSet(1) << parent
	bits := func(kinds ...codec.CodecType) excludeSet {
		var result excludeSet
		return result.With(kinds...)
	}
	switch parent {
	case codec.CodecTypeDict:
		if childIndex == 0 { // dictionary values
			return self | bits(codec.CodecTypeRunEnd, codec.CodecTypeSparse)
		}
		return self | bits(codec.CodecTypeFor, codec.CodecTypeSequence, codec.CodecTypeZigZag, codec.CodecTypeDelta)
	case codec.CodecTypeRunEnd:
		if childIndex == 1 { // run ends
			return self | bits(codec.CodecTypeDict, codec.CodecTypeSparse)
		}
	case codec.CodecTypeZigZag:
		return self | bits(codec.CodecTypeDict, codec.CodecTypeRunEnd, codec.CodecTypeSparse)
	case codec.CodecTypeDelta:
		return self | bits(codec.CodecTypeDict, codec.CodecTypeRunEnd, codec.CodecTypeSparse, codec.CodecTypeSequence, codec.CodecTypeZigZag)
	case codec.CodecTypeSparse:
		if childIndex == 1 { // patch indices
			return self | bits(codec.CodecTypeDict, codec.CodecTypeRunEnd)
		}
	case codec.CodecTypeALP:
		if childIndex == 1 { // patch indices
			return self | bits(codec.CodecTypeDict, codec.CodecTypeRunEnd, codec.CodecTypeSparse)
		}
	case codec.CodecTypeALPRD:
		return self | bits(codec.CodecTypeDict, codec.CodecTypeRunEnd, codec.CodecTypeSparse)
	case codec.CodecTypeFSST:
		if childIndex == 0 { // offsets
			return self | bits(codec.CodecTypeDict, codec.CodecTypeRunEnd, codec.CodecTypeSparse)
		}
	}
	return self
}
