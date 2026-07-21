package btrblocks

func childExclusions(parent CodecType, childIndex uint8) excludeSet {
	self := excludeSet(1) << parent
	bits := func(kinds ...CodecType) excludeSet {
		var result excludeSet
		return result.With(kinds...)
	}
	switch parent {
	case CodecTypeDict:
		if childIndex == 0 { // dictionary values
			return self | bits(CodecTypeRunEnd, CodecTypeSparse)
		}
		return self | bits(CodecTypeFor, CodecTypeSequence, CodecTypeZigZag, CodecTypeDelta)
	case CodecTypeRunEnd:
		if childIndex == 1 { // run ends
			return self | bits(CodecTypeDict, CodecTypeSparse)
		}
	case CodecTypeZigZag:
		return self | bits(CodecTypeDict, CodecTypeRunEnd, CodecTypeSparse)
	case CodecTypeDelta:
		return self | bits(CodecTypeDict, CodecTypeRunEnd, CodecTypeSparse, CodecTypeSequence, CodecTypeZigZag)
	case CodecTypeSparse:
		if childIndex == 1 { // patch indices
			return self | bits(CodecTypeDict, CodecTypeRunEnd)
		}
	case CodecTypeALP:
		if childIndex == 1 { // patch indices
			return self | bits(CodecTypeDict, CodecTypeRunEnd, CodecTypeSparse)
		}
	case CodecTypeALPRD:
		return self | bits(CodecTypeDict, CodecTypeRunEnd, CodecTypeSparse)
	case CodecTypeFSST:
		if childIndex == 0 { // offsets
			return self | bits(CodecTypeDict, CodecTypeRunEnd, CodecTypeSparse)
		}
	}
	return self
}
