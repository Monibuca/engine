package mpegps

import (
	"encoding/binary"
	"errors"
	"io"
)

type IOBuffer struct {
	buf []byte // contents are the bytes buf[off : len(buf)]
	off int    // read at &buf[off], write at &buf[len(buf)]
}

func (b *IOBuffer) Next(n int) []byte {
	m := b.Len()
	if n > m {
		n = m
	}
	data := b.buf[b.off : b.off+n]
	b.off += n
	return data
}
func (b *IOBuffer) Uint16() (uint16, error) {
	if b.Len() > 1 {

		return binary.BigEndian.Uint16(b.Next(2)), nil
	}
	return 0, io.EOF
}

func (b *IOBuffer) Skip(n int) (err error) {
	_, err = b.ReadN(n)
	return
}

func (b *IOBuffer) Uint32() (uint32, error) {
	if b.Len() > 3 {
		return binary.BigEndian.Uint32(b.Next(4)), nil
	}
	return 0, io.EOF
}

func (b *IOBuffer) ReadN(length int) ([]byte, error) {
	if b.Len() >= length {
		return b.Next(length), nil
	}
	return nil, io.EOF
}

//func (b *IOBuffer) Read(buf []byte) (n int, err error) {
//	var ret []byte
//	ret, err = b.ReadN(len(buf))
//	copy(buf, ret)
//	return len(ret), err
//}

// empty reports whether the unread portion of the buffer is empty.
func (b *IOBuffer) empty() bool { return b.Len() <= b.off }

func (b *IOBuffer) ReadByte() (byte, error) {
	if b.empty() {
		// Buffer is empty, reset to recover space.
		b.Reset()
		return 0, io.EOF
	}
	c := b.buf[b.off]
	b.off++
	return c, nil
}

func (b *IOBuffer) Reset() {
	b.buf = b.buf[:0]
	b.off = 0
}

func (b *IOBuffer) Len() int { return len(b.buf) - b.off }

// tryGrowByReslice is a inlineable version of grow for the fast-case where the
// internal buffer only needs to be resliced.
// It returns the index where bytes should be written and whether it succeeded.
func (b *IOBuffer) tryGrowByReslice(n int) (int, bool) {
	if l := len(b.buf); n <= cap(b.buf)-l {
		b.buf = b.buf[:l+n]
		return l, true
	}
	return 0, false
}

var ErrTooLarge = errors.New("IOBuffer: too large")

func (b *IOBuffer) Write(p []byte) (n int, err error) {
	l := copy(b.buf, b.buf[b.off:])
	b.buf = append(b.buf[:l], p...)
	b.off = 0
	// println(b.buf, b.off, b.buf[b.off], b.buf[b.off+1], b.buf[b.off+2], b.buf[b.off+3])
	return len(p), nil
	// defer func() {
	// 	if recover() != nil {
	// 		panic(ErrTooLarge)
	// 	}
	// }()
	// l := len(p)
	// oldLen := len(b.buf)
	// m, ok := b.tryGrowByReslice(l)
	// if !ok {
	// 	m = oldLen - b.off
	// 	buf := append(append(([]byte)(nil), b.buf[b.off:]...), p...)
	// 	b.off = 0
	// 	b.buf = buf
	// }
	// return copy(b.buf[m:], p), nil
}
