package util

type Integer interface {
	~int | ~int16 | ~int32 | ~int64 | ~uint | ~uint16 | ~uint32 | ~uint64
}

func PutBE[T Integer](b []byte, num T) []byte {
	for i, n := 0, len(b); i < n; i++ {
		b[i] = byte(num >> ((n - i - 1) << 3))
	}
	return b
}

func ReadBE[T Integer](b []byte) (num T) {
	num = 0
	for i, n := 0, len(b); i < n; i++ {
		num += T(b[i]) << ((n - i - 1) << 3)
	}
	return
}

func GetBE[T Integer](b []byte, num *T) T {
	*num = 0
	for i, n := 0, len(b); i < n; i++ {
		*num += T(b[i]) << ((n - i - 1) << 3)
	}
	return *num
}
