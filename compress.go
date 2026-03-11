package btrblocks

const defaultDepth = 3

func compress[T Integer | Float | String](data []T, depth int) Codec[T] {
	var zero T
	switch any(zero).(type) {
	case int8:
		v := any(data).([]int8)
		return any(CompressInteger(v, depth)).(Codec[T])
	case int16:
		v := any(data).([]int16)
		return any(CompressInteger(v, depth)).(Codec[T])
	case int32:
		v := any(data).([]int32)
		return any(CompressInteger(v, depth)).(Codec[T])
	case int64:
		v := any(data).([]int64)
		return any(CompressInteger(v, depth)).(Codec[T])
	case uint8:
		v := any(data).([]uint8)
		return any(CompressInteger(v, depth)).(Codec[T])
	case uint16:
		v := any(data).([]uint16)
		return any(CompressInteger(v, depth)).(Codec[T])
	case uint32:
		v := any(data).([]uint32)
		return any(CompressInteger(v, depth)).(Codec[T])
	case uint64:
		v := any(data).([]uint64)
		return any(CompressInteger(v, depth)).(Codec[T])
	case float32:
		v := any(data).([]float32)
		return any(CompressFloat(v, depth)).(Codec[T])
	case float64:
		v := any(data).([]float64)
		return any(CompressFloat(v, depth)).(Codec[T])
	case string:
		v := any(data).([]string)
		return any(CompressString(v, depth)).(Codec[T])
	default:
		return nil
	}
}

func Compress[T Integer | Float | String](data []T) Codec[T] {
	return compress(data, defaultDepth)
}

func CompressWithDepth[T Integer | Float | String](data []T, depth int) Codec[T] {
	return compress(data, depth)
}
