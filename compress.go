package btrblocks

func Compress[T Integer | Float | String](data []T, exclude []TypeIntCodec | []TypeFloatCodec | []TypeStringCodec) Codec[T] {
	var zero T
	switch any(zero).(type) {
	case int8:
		return CompressInteger[int8](data, exclude)
	case int16:
		return CompressInteger[int16](data, exclude)
	case int32:
		return CompressInteger[int32](data, exclude)
	case int64:
		return CompressInteger[int64](data, exclude)
	case uint8:
		return CompressInteger[uint8](data, exclude)
	case uint16:
		return CompressInteger[uint16](data, exclude)
	case uint32:
		return CompressInteger[uint32](data, exclude)
	case uint64:
		return CompressInteger[uint64](data, exclude)
	case float32:
		return CompressFloat[float32](data, exclude)
	case float64:
		return CompressFloat[float64](data, exclude)
	case string:
		return CompressString[string](data, exclude)
	default:
		return nil
	}
}