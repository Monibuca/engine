package util

type Slice[T comparable] []T

func (s Slice[T]) Len() int {
	return len(s)
}

func (s *Slice[T]) Add(v T) {
	*s = append(*s, v)
}

func (s *Slice[T]) Delete(v T) bool {
	for i, val := range *s {
		if val == v {
			*s = append((*s)[:i], (*s)[i+1:]...)
			return true
		}
	}
	return false
}
