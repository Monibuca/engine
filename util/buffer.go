package util

import (
	"encoding/binary"
	"math"
)

type Buffer []byte

func (b *Buffer) ReadN(n int) Buffer {
	l := b.Len()
	r := (*b)[:n]
	*b = (*b)[n:l]
	return r
}
func (b *Buffer) ReadFloat64() float64 {
	return math.Float64frombits(b.ReadUint64())
}
func (b *Buffer) ReadUint64() uint64 {
	return binary.BigEndian.Uint64(b.ReadN(8))
}
func (b *Buffer) ReadUint32() uint32 {
	return binary.BigEndian.Uint32(b.ReadN(4))
}
func (b *Buffer) ReadUint24() uint32 {
	return ReadBE[uint32](b.ReadN(3))
}
func (b *Buffer) ReadUint16() uint16 {
	return binary.BigEndian.Uint16(b.ReadN(2))
}
func (b *Buffer) ReadByte() byte {
	return b.ReadN(1)[0]
}
func (b *Buffer) WriteFloat64(v float64) {
	PutBE(b.Malloc(8), math.Float64bits(v))
}
func (b *Buffer) WriteUint32(v uint32) {
	binary.BigEndian.PutUint32(b.Malloc(4), v)
}
func (b *Buffer) WriteUint24(v uint32) {
	PutBE(b.Malloc(3), v)
}
func (b *Buffer) WriteUint16(v uint16) {
	binary.BigEndian.PutUint16(b.Malloc(2), v)
}
func (b *Buffer) WriteByte(v byte) {
	b.Malloc(1)[0] = v
}
func (b *Buffer) WriteString(a string) {
	*b = append(*b, a...)
}
func (b *Buffer) Write(a []byte) (n int, err error) {
	*b = append(*b, a...)
	return len(a), nil
}
func (b Buffer) Len() int {
	return len(b)
}
func (b Buffer) CanRead() bool {
	return b.Len() > 0
}
func (b Buffer) Cap() int {
	return cap(b)
}
func (b Buffer) SubBuf(start int, length int) Buffer {
	return b[start : start+length]
}

func (b *Buffer) Malloc(count int) Buffer {
	l := b.Len()
	newL := l + count
	if newL > b.Cap() {
		n := make(Buffer, newL)
		copy(n, *b)
		*b = n
	} else {
		*b = b.SubBuf(0, newL)
	}
	return b.SubBuf(l, count)
}
func (b *Buffer) Reset() {
	*b = b.SubBuf(0, 0)
}
func (b *Buffer) Glow(n int) {
	l := b.Len()
	b.Malloc(n)
	*b = b.SubBuf(0, l)
}

// ConcatBuffers 合并碎片内存为一个完整内存
func ConcatBuffers[T ~[]byte](input []T) (out []byte) {
	for _, v := range input {
		out = append(out, v...)
	}
	return
}

// SizeOfBuffers 计算Buffers的内容长度
func SizeOfBuffers[T ~[]byte](buf []T) (size int) {
	for _, b := range buf {
		size += len(b)
	}
	return
}

// SplitBuffers 按照一定大小分割 Buffers
func SplitBuffers[T ~[]byte](buf []T, size int) (result [][]T) {
	buf = append([]T(nil), buf...)
	for total := SizeOfBuffers(buf); total > 0; {
		if total <= size {
			return append(result, buf)
		} else {
			var before []T
			sizeOfBefore := 0
			for _, b := range buf {
				need := size - sizeOfBefore
				if lenOfB := len(b); lenOfB > need {
					before = append(before, b[:need])
					result = append(result, before)
					total -= need
					buf[0] = b[need:]
					break
				} else {
					sizeOfBefore += lenOfB
					before = append(before, b)
					total -= lenOfB
					buf = buf[1:]
				}
			}
		}
	}
	return
}
