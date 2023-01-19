package util

import "net"

type BytesLinkList struct {
	Head       *BytesLinkItem
	Tail       *BytesLinkItem
	Length     int
	ByteLength int
}

func (list *BytesLinkList) Push(item *BytesLinkItem) {
	if list == nil {
		return
	}
	if list.Head == nil {
		list.Head = item
		list.Tail = item
		list.Length = 1
		list.ByteLength = item.Len()
		return
	}
	list.Tail.Next = item
	list.Tail = item
	list.Length++
	list.ByteLength += item.Len()
}

func (list *BytesLinkList) Shift() (item *BytesLinkItem) {
	if list.Head == nil {
		return nil
	}
	item = list.Head
	list.Head = list.Head.Next
	list.Length--
	list.ByteLength -= item.Len()
	return
}

func (list *BytesLinkList) ToBuffers() (result net.Buffers) {
	for p := list.Head; p != nil; p = p.Next {
		result = append(result, p.Bytes)
	}
	return
}

// 全部回收掉
func (list *BytesLinkList) Recycle() {
	for p := list.Head; p != nil; p = p.Next {
		p.Pool.Push(p)
	}
	list.Head = nil
	list.Tail = nil
	list.Length = 0
	list.ByteLength = 0
}

type BytesLinkItem struct {
	Next  *BytesLinkItem
	Bytes []byte
	Pool  *BytesLinkList
}

func (b *BytesLinkItem) Len() int {
	return len(b.Bytes)
}

func (b *BytesLinkItem) Recycle() {
	b.Pool.Push(b)
}

func (b *BytesLinkItem) ToBuffers() (result net.Buffers) {
	for p := b; p != nil; p = p.Next {
		result = append(result, p.Bytes)
	}
	return
}

type BytesPool []BytesLinkList

// 获取来自真实内存的切片的——假内存块，即只回收外壳
func (p BytesPool) GetFake() (item *BytesLinkItem) {
	if p[0].Length > 0 {
		return p[0].Shift()
	} else {
		return &BytesLinkItem{
			Pool: &p[0],
		}
	}
}

func (p BytesPool) Get(size int) (item *BytesLinkItem) {
	for i := 1; i < len(p); i++ {
		level := 1 << i
		if level >= size {
			if p[i].Length > 0 {
				item = p[i].Shift()
				item.Bytes = item.Bytes[:size]
			} else {
				item = &BytesLinkItem{
					Bytes: make([]byte, size, level),
					Pool:  &p[i],
				}
			}
		}
	}
	if item == nil {
		item = &BytesLinkItem{
			Bytes: make([]byte, size),
		}
	}
	return
}
