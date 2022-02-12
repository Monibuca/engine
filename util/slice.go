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

func (s *Slice[T]) Reset() {
	if len(*s) > 0 {
		*s = (*s)[:0]
	}
}

func (s *Slice[T]) ResetAppend(first T) {
	s.Reset()
	s.Add(first)
}

func LastElement[T any](s []T) T {
	return s[len(s)-1]
}
