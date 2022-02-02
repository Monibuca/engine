package util

import "constraints"

func PutBE[T constraints.Integer](b []byte, num T) []byte {
	for i, n := 0, len(b); i < n; i++ {
		b[i] = byte(num >> ((n - i - 1) << 3))
	}
	return b
}

func ReadBE[T constraints.Integer](b []byte) (num T) {
	for i, n := 0, len(b); i < n; i++ {
		num += T(b[i]) << ((n - i - 1) << 3)
	}
	return
}
