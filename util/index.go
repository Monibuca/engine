package util

func Clone[T any](x T) *T {
	return &x
}