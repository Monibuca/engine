package util

type BytesPool [][]byte

func (pool *BytesPool) Get(size int) (result []byte) {
	if l := len(*pool); l > 0 {
		result = (*pool)[l-1]
		*pool = (*pool)[:l-1]
	} else {
		result = make([]byte, size, 10)
	}
	return
}

func (pool *BytesPool) Put(b []byte) {
	*pool = append(*pool, b)
}
