package util

type Buffer []byte

func (b *Buffer) Write(a []byte) (n int, err error) {
	*b = append(*b, a...)
	return len(a), nil
}
func (b Buffer) Len() int {
	return len(b)
}
func (b Buffer) Cap() int {
	return cap(b)
}
func (b Buffer) SubBuf(start int, length int) Buffer {
	return b[start : start+length]
}

func (b *Buffer) Malloc(count int) Buffer {
	l := b.Len()
	if l+count > b.Cap() {
		n := make(Buffer, l+count)
		copy(n, *b)
		*b = n
	}
	return b.SubBuf(l, count)
}
func (b *Buffer) Reset() {
	*b = b.SubBuf(0, 0)
}
