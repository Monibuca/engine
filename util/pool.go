package util

import (
	"io"
	"net"
	"sync"
)

type Recyclable interface {
	Recycle()
}
type BLLReader struct {
	*ListItem[Buffer]
	pos int
}

func (r *BLLReader) CanRead() bool {
	return r.ListItem != nil && !r.IsRoot()
}

func (r *BLLReader) Skip(n int) (err error) {
	for r.CanRead() {
		l := r.Value.Len() - r.pos
		if l > n {
			r.pos += n
			return
		}
		n -= l
		r.ListItem = r.Next
		r.pos = 0
	}
	return io.EOF
}

func (r *BLLReader) ReadByte() (b byte, err error) {
	for r.CanRead() {
		l := r.Value.Len() - r.pos
		if l > 0 {
			b = r.Value[r.pos]
			r.pos++
			return
		}
		r.ListItem = r.Next
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
	for r.CanRead() {
		l := r.Value.Len() - r.pos
		if l > n {
			result = append(result, r.Value[r.pos:r.pos+n])
			r.pos += n
			return
		}
		result = append(result, r.Value[r.pos:])
		n -= l
		r.ListItem = r.Next
		r.pos = 0
	}
	return
}

func (r *BLLReader) GetOffset() int {
	return r.pos
}

type BLLsReader struct {
	*ListItem[*BLL]
	BLLReader
}

func (r *BLLsReader) CanRead() bool {
	return r.ListItem != nil && !r.IsRoot()
}

func (r *BLLsReader) ReadByte() (b byte, err error) {
	if r.BLLReader.CanRead() {
		b, err = r.BLLReader.ReadByte()
		if err == nil {
			return
		}
	}
	r.ListItem = r.Next
	if !r.CanRead() {
		return 0, io.EOF
	}
	r.BLLReader = *r.Value.NewReader()
	return r.BLLReader.ReadByte()
}

type BLLs struct {
	List[*BLL]
	ByteLength int
}

func (list *BLLs) PushValue(item *BLL) {
	if list == nil {
		return
	}
	list.List.PushValue(item)
	list.ByteLength += item.ByteLength
}

func (list *BLLs) PushShell(b []byte) {
	list.Push(GetShell(b))
}

func (list *BLLs) Push(item *ListItem[Buffer]) {
	if list == nil {
		return
	}
	if list.List.Length == 0 {
		var bll BLL
		bll.Push(item)
		list.PushValue(&bll)
	} else {
		list.Pre.Value.Push(item)
	}
	list.ByteLength += item.Value.Len()
}

func (list *BLLs) ToList() (result [][][]byte) {
	list.Range(func(bll *BLL) bool {
		result = append(result, bll.ToBuffers())
		return true
	})
	return
}

func (list *BLLs) ToBuffers() (result net.Buffers) {
	list.Range(func(bll *BLL) bool {
		result = append(result, bll.ToBuffers()...)
		return true
	})
	return
}

func (list *BLLs) ToBytes() (result []byte) {
	list.Range(func(bll *BLL) bool {
		result = append(result, bll.ToBytes()...)
		return true
	})
	return
}

func (list *BLLs) Recycle() {
	list.Range(func(bll *BLL) bool {
		bll.Recycle()
		return true
	})
	list.Clear()
	list.ByteLength = 0
}

func (list *BLLs) NewReader() *BLLsReader {
	return &BLLsReader{list.Next, *list.Next.Value.NewReader()}
}

// ByteLinkList
type BLL struct {
	List[Buffer]
	ByteLength int
}

func (list *BLL) NewReader() *BLLReader {
	return &BLLReader{list.Next, 0}
}

//	func (list *BLL) Concat(list2 BLL) {
//		list.Tail.Next = list2.Head
//		list.Tail = list2.Tail
//		list.Length += list2.Length
//		list.ByteLength += list2.ByteLength
//	}

func (list *BLL) PushShell(b Buffer) {
	list.Push(GetShell(b))
}

func (list *BLL) Push(item *ListItem[Buffer]) {
	if list == nil {
		return
	}
	list.List.Push(item)
	list.ByteLength += item.Value.Len()
}

func (list *BLL) Shift() (item *ListItem[Buffer]) {
	if list == nil || list.Length == 0 {
		return
	}
	item = list.List.Shift()
	list.ByteLength -= item.Value.Len()
	return
}

func (list *BLL) Clear() {
	list.List.Clear()
	list.ByteLength = 0
}

func (list *BLL) ToBuffers() (result net.Buffers) {
	list.Range(func(item Buffer) bool {
		result = append(result, item)
		return true
	})
	return
}

func (list *BLL) WriteTo(w io.Writer) (int64, error) {
	t := list.ToBuffers()
	return t.WriteTo(w)
}

func (list *BLL) ToBytes() (b []byte) {
	b = make([]byte, 0, list.ByteLength)
	list.Range(func(item Buffer) bool {
		b = append(b, item...)
		return true
	})
	return
}

// 全部回收掉
func (list *BLL) Recycle() {
	list.List.Recycle()
	list.ByteLength = 0
}

func (list *BLL) GetByte(index int) (b byte) {
	list.Range(func(item Buffer) bool {
		l := item.Len()
		if index < l {
			b = item[index]
			return false
		}
		index -= l
		return true
	})
	return
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

// func (b *Buffer) ToBuffers() (result net.Buffers) {
// 	for p := b.Next; p != &b.ListItem; p = p.Next {
// 		result = append(result, p.Value)
// 	}
// 	return
// }

type BytesPool []*sync.Pool

var DefaultBytesPool = make(BytesPool, 17)

func init() {
	for i := 0; i < len(DefaultBytesPool); i++ {
		pool := &sync.Pool{}
		pool.New = func() interface{} {
			return &ListItem[Buffer]{Value: make(Buffer, 1<<i), Pool: pool}
		}
		DefaultBytesPool[i] = pool
	}
}

func GetShell(b []byte) (item *ListItem[Buffer]) {
	return DefaultBytesPool.GetShell(b)
}

func GetBLI(size int) *ListItem[Buffer] {
	return DefaultBytesPool.Get(size)
}

// 获取来自真实内存的切片的——假内存块，即只回收外壳
func (p BytesPool) GetShell(b []byte) (item *ListItem[Buffer]) {
	if len(p) == 0 {
		return &ListItem[Buffer]{Value: b}
	}
	item = p[0].Get().(*ListItem[Buffer])
	item.Pool = p[0]
	item.Value = b
	return
}

func (p BytesPool) Get(size int) (mem *ListItem[Buffer]) {
	for i := 1; i < len(p); i++ {
		if level := 1 << i; level >= size {
			mem = p[i].Get().(*ListItem[Buffer])
			mem.Pool = p[i]
			mem.Value.Relloc(size)
			return
		}
	}
	// Pool 中没有就无法回收
	if mem == nil {
		mem = &ListItem[Buffer]{
			Value: make(Buffer, size),
		}
	}
	return
}

type Pool[T any] sync.Pool

func (p *Pool[T]) Get() (v *T) {
	return ((*sync.Pool)(p)).Get().(*T)
}

func (p *Pool[T]) Put(v *T) {
	((*sync.Pool)(p)).Put(v)
}
