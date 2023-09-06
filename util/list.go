package util

// 带回收功能的泛型双向链表

type IList[T any] interface {
	Push(*ListItem[T])
	Shift() *ListItem[T]
	Clear()
}

type ListItem[T any] struct {
	Value     T
	Next, Pre *ListItem[T] `json:"-" yaml:"-"`
	Pool      *List[T]     `json:"-" yaml:"-"` // 回收池
	list      *List[T]
	reset     bool // 是否需要重置
}

func (item *ListItem[T]) InsertBefore(insert *ListItem[T]) {
	if insert.list != nil {
		panic("item already in list")
	}
	item.Pre.InsertAfter(insert)
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
	hasPool := item.list != item.Pool && item.Pool != nil && item.Pool.Length < PoolSize
	if item.reset || !hasPool {
		var null T
		item.Value = null
	}
	if hasPool {
		item.Pool.Push(item)
	} else {
		item.Pool = nil
		item.list = nil
		item.Next = nil
		item.Pre = nil
	}
}

func (item *ListItem[T]) Range(do func(value T) bool) {
	item.RangeItem(func(item *ListItem[T]) bool {
		return do(item.Value)
	})
}

func (item *ListItem[T]) RangeItem(do func(*ListItem[T]) bool) {
	for ; item != nil && item.list != nil && !item.IsRoot() && do(item); item = item.Next {
	}
}

type List[T any] struct {
	ListItem[T]
	Length int
}

func (p *List[T]) PushValue(value T) {
	p.Push(&ListItem[T]{Value: value, reset: true})
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
	if p.Length > 0 {
		p.Next.Range(do)
	}
}

func (p *List[T]) RangeItem(do func(*ListItem[T]) bool) {
	if p.Length > 0 {
		p.Next.RangeItem(do)
	}
}

func (p *List[T]) Recycle() {
	for item := p.Shift(); item != nil; item = p.Shift() {
		item.Recycle()
	}
	if p.Length != 0 {
		panic("recycle list error")
	}
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
