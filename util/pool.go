package util

import (
	"io"
	"net"
)

type BLLReader struct {
	*BLI
	pos int
}

func (r *BLLReader) CanRead() bool {
	return r.BLI != nil
}

func (r *BLLReader) Skip(n int) (err error) {
	for r.BLI != nil {
		l := r.Len() - r.pos
		if l > n {
			r.pos += n
			return
		}
		n -= l
		r.BLI = r.Next
		r.pos = 0
	}
	return io.EOF
}

func (r *BLLReader) ReadByte() (b byte, err error) {
	for r.BLI != nil {
		l := r.Len() - r.pos
		if l > 0 {
			b = r.Bytes[r.pos]
			r.pos++
			return
		}
		r.BLI = r.Next
		r.pos = 0
	}
	return 0, io.EOF
}

func (r *BLLReader) ReadBE(n int) (be uint32, err error) {
	for i := 0; i < n; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		be += uint32(b) << ((n - i - 1) << 3)
	}
	return
}

func (r *BLLReader) ReadN(n int) (result net.Buffers) {
	for r.BLI != nil {
		l := r.Len() - r.pos
		if l > n {
			result = append(result, r.Bytes[r.pos:r.pos+n])
			r.pos += n
			return
		}
		result = append(result, r.Bytes[r.pos:])
		n -= l
		r.BLI = r.Next
		r.pos = 0
	}
	return
}

type BLLs struct {
	Head       *BLL
	Tail       *BLL
	Length     int
	ByteLength int
}

func (list *BLLs) Push(item *BLL) {
	if list == nil {
		return
	}
	if list.Head == nil {
		list.Head = item
		list.Tail = item
		list.Length = 1
		list.ByteLength = item.ByteLength
		return
	}
	list.Tail.Next = item
	list.Tail = item
	list.Length++
	list.ByteLength += item.ByteLength
}

func (list *BLLs) PushItem(item *BLI) {
	if list == nil {
		return
	}
	if list.Head == nil {
		list.Head = &BLL{}
		list.Tail = list.Head
		list.Length = 1
	}
	list.Tail.Push(item)
	list.ByteLength += item.Len()
}

func (list *BLLs) ToList() (result [][][]byte) {
	for p := list.Head; p != nil; p = p.Next {
		result = append(result, p.ToBuffers())
	}
	return
}

func (list *BLLs) ToBytes() (result []byte) {
	for p := list.Head; p != nil; p = p.Next {
		result = append(result, p.ToBytes()...)
	}
	return
}

func (list *BLLs) Recycle() {
	for au := list.Head; au != nil; au = au.Next {
		au.Recycle()
	}
	list.Head = nil
	list.Tail = nil
	list.Length = 0
	list.ByteLength = 0
}

type BLL struct {
	Head       *BLI
	Tail       *BLI
	Length     int
	ByteLength int
	Next       *BLL
}

func (list *BLL) NewReader() *BLLReader {
	return &BLLReader{list.Head, 0}
}

func (list *BLL) Concat(list2 BLL) {
	list.Tail.Next = list2.Head
	list.Tail = list2.Tail
	list.Length += list2.Length
	list.ByteLength += list2.ByteLength
}

func (list *BLL) Push(item *BLI) {
	if list == nil {
		return
	}
	if list.Head == nil {
		list.Head = item
	} else {
		list.Tail.Next = item
	}
	list.Tail = item
	list.Tail.Next = nil
	list.Length++
	list.ByteLength += item.Len()
}

func (list *BLL) Shift() (item *BLI) {
	if list.Head == nil {
		return nil
	}
	item = list.Head
	list.Head = item.Next
	item.Next = nil
	list.Length--
	list.ByteLength -= item.Len()
	return
}

func (list *BLL) ToBuffers() (result net.Buffers) {
	for p := list.Head; p != nil; p = p.Next {
		result = append(result, p.Bytes)
	}
	return
}

func (list *BLL) WriteTo(w io.Writer) (int64, error) {
	t := list.ToBuffers()
	return t.WriteTo(w)
}

func (list *BLL) ToBytes() (b []byte) {
	b = make([]byte, 0, list.ByteLength)
	for p := list.Head; p != nil; p = p.Next {
		b = append(b, p.Bytes...)
	}
	return
}

// 全部回收掉
func (list *BLL) Recycle() {
	for p := list.Head; p != nil; p = p.Next {
		p.Recycle()
	}
	list.Head = nil
	list.Tail = nil
	list.Length = 0
	list.ByteLength = 0
	list.Next = nil
}

func (list *BLL) GetByte(index int) byte {
	for p := list.Head; p != nil; p = p.Next {
		l := p.Len()
		if index < l {
			return p.Bytes[index]
		}
		index -= l
	}
	return 0
}

func (list *BLL) GetUint24(index int) uint32 {
	return list.GetUintN(index, 3)
}

func (list *BLL) GetUintN(index int, n int) (result uint32) {
	for i := 0; i < n; i++ {
		result += uint32(list.GetByte(index+i)) << ((n - i - 1) << 3)
	}
	return
}

type BLI struct {
	Next  *BLI
	Bytes []byte
	Pool  *BLL
}

func (b *BLI) Len() int {
	return len(b.Bytes)
}

func (b *BLI) Recycle() {
	b.Next = nil
	b.Pool.Push(b)
	b.Pool = nil //防止重复回收
}

func (b *BLI) ToBuffers() (result net.Buffers) {
	for p := b; p != nil; p = p.Next {
		result = append(result, p.Bytes)
	}
	return
}

type BytesPool []BLL

// 获取来自真实内存的切片的——假内存块，即只回收外壳
func (p BytesPool) GetShell(b []byte) (item *BLI) {
	if p[0].Length > 0 {
		item = p[0].Shift()
		item.Bytes = b
		item.Pool = &p[0]
		return
	} else {
		return &BLI{
			Pool:  &p[0],
			Bytes: b,
		}
	}
}

func (p BytesPool) Get(size int) (item *BLI) {
	for i := 1; i < len(p); i++ {
		level := 1 << i
		if level >= size {
			if p[i].Length > 0 {
				item = p[i].Shift()
				item.Bytes = item.Bytes[:size]
				item.Pool = &p[i]
			} else {
				item = &BLI{
					Bytes: make([]byte, size, level),
					Pool:  &p[i],
				}
			}
			return
		}
	}
	// Pool 中没有就无法回收
	if item == nil {
		item = &BLI{
			Bytes: make([]byte, size),
		}
	}
	return
}
