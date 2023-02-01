package util

// 带回收功能的泛型双向链表

type IList[T any] interface {
	Push(*ListItem[T])
	Shift() *ListItem[T]
	Clear()
}

type ListItem[T any] struct {
	Value     T
	Next, Pre *ListItem[T]
	Pool      *List[T] // 回收池
	list      *List[T]
}

func (item *ListItem[T]) InsertBefore(insert *ListItem[T]) {
	if insert.list != nil {
		panic("item already in list")
	}
	insert.list = item.list
	insert.Pre = item.Pre
	insert.Next = item
	item.Pre.Next = insert
	item.Pre = insert
	item.list.Length++
}
func (item *ListItem[T]) InsertBeforeValue(value T) (result *ListItem[T]) {
	result = &ListItem[T]{Value: value}
	item.InsertBefore(result)
	return
}
func (item *ListItem[T]) InsertAfter(insert *ListItem[T]) {
	if insert.list != nil {
		panic("item already in list")
	}
	insert.list = item.list
	insert.Next = item.Next
	insert.Pre = item
	item.Next.Pre = insert
	item.Next = insert
	item.list.Length++
}

func (item *ListItem[T]) InsertAfterValue(value T) (result *ListItem[T]) {
	result = &ListItem[T]{Value: value}
	item.InsertAfter(result)
	return
}

func (item *ListItem[T]) IsRoot() bool {
	return &item.list.ListItem == item
}

func (item *ListItem[T]) Recycle() {
	if item.list != item.Pool && item.Pool != nil {
		item.Pool.Push(item)
	}
}

func (item *ListItem[T]) Range(do func(value T) bool) {
	for ; item != nil && !item.IsRoot() && do(item.Value); item = item.Next {
	}
}

func (item *ListItem[T]) RangeItem(do func(*ListItem[T]) bool) {
	for ; item != nil && !item.IsRoot() && do(item); item = item.Next {
	}
}

type List[T any] struct {
	ListItem[T]
	Length int
}

func (p *List[T]) PushValue(value T) {
	p.Push(&ListItem[T]{Value: value})
}

func (p *List[T]) Push(item *ListItem[T]) {
	if item.list != nil {
		panic("item already in list")
	}
	if p.Length == 0 {
		p.Next = &p.ListItem
		p.Pre = &p.ListItem
		p.ListItem.list = p
	}
	p.Pre.InsertAfter(item)
	// item.list = p
	// item.Next = &p.ListItem
	// item.Pre = p.Pre
	// // p.Value = item.Value
	// p.Pre.Next = item
	// p.Pre = p.Pre.Next
	// p.Length++
}

func (p *List[T]) UnshiftValue(value T) {
	p.Unshift(&ListItem[T]{Value: value})
}

func (p *List[T]) Unshift(item *ListItem[T]) {
	if item.list != nil {
		panic("item already in list")
	}
	if p.Length == 0 {
		p.Next = &p.ListItem
		p.Pre = &p.ListItem
		p.ListItem.list = p
	}
	p.Next.InsertBefore(item)
	// item.list = p
	// item.Next = p.Next
	// item.Pre = &p.ListItem
	// // p.Value = item.Value
	// p.Next.Pre = item
	// p.Next = p.Next.Pre
	// p.Length++
}

func (p *List[T]) ShiftValue() T {
	return p.Shift().Value
}

func (p *List[T]) PoolShift() (head *ListItem[T]) {
	if head = p.Shift(); head == nil {
		head = &ListItem[T]{Pool: p}
	}
	return
}

func (p *List[T]) Shift() (head *ListItem[T]) {
	if p.Length == 0 {
		return
	}
	head = p.Next
	p.Next = head.Next
	head.Pre = nil
	head.Next = nil
	head.list = nil
	p.Length--
	return
}

func (p *List[T]) Clear() {
	p.Next = &p.ListItem
	p.Pre = &p.ListItem
	p.Length = 0
}

func (p *List[T]) Range(do func(value T) bool) {
	p.Next.Range(do)
}

func (p *List[T]) RangeItem(do func(*ListItem[T]) bool) {
	p.Next.RangeItem(do)
}

func (p *List[T]) Recycle() {
	for item := p.Shift(); item != nil; item = p.Shift() {
		item.Recycle()
	}
	if p.Length != 0 {
		panic("recycle list error")
	}
	// for item := p.Next; item != nil && item.list != nil && !item.IsRoot(); {
	// 	next := item.Next
	// 	item.Recycle()
	// 	item = next
	// }
	// p.Clear()
}

// Transfer 把链表中的所有元素转移到另一个链表中
func (p *List[T]) Transfer(target IList[T]) {
	for item := p.Shift(); item != nil; item = p.Shift() {
		target.Push(item)
	}
	if p.Length != 0 {
		panic("transfer list error")
	}
}
