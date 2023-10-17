package common

import (
	"sync/atomic"

	"m7s.live/engine/v4/util"
)

type emptyLocker struct{}

func (emptyLocker) Lock()   {}
func (emptyLocker) Unlock() {}

var EmptyLocker emptyLocker

type RingWriter[T any, F IDataFrame[T]] struct {
	*util.Ring[F] `json:"-" yaml:"-"`
	ReaderCount   atomic.Int32 `json:"-" yaml:"-"`
	pool          *util.Ring[F]
	poolSize      int
	Size          int
	LastValue     F
	constructor   func() F
}

func (rb *RingWriter[T, F]) create(n int) (ring *util.Ring[F]) {
	ring = util.NewRing[F](n)
	for p, i := ring, n; i > 0; p, i = p.Next(), i-1 {
		p.Value = rb.constructor()
		p.Value.Init()
	}
	return
}

func (rb *RingWriter[T, F]) Init(n int, constructor func() F) *RingWriter[T, F] {
	rb.constructor = constructor
	rb.Ring = rb.create(n)
	rb.Size = n
	rb.LastValue = rb.Value
	return rb
}

// func (rb *RingBuffer[T, F]) MoveNext() F {
// 	rb.LastValue = rb.Value
// 	rb.Ring = rb.Next()
// 	return rb.Value
// }

func (rb *RingWriter[T, F]) Glow(size int) (newItem *util.Ring[F]) {
	if size < rb.poolSize {
		newItem = rb.pool.Unlink(size)
		rb.poolSize -= size
	} else if size == rb.poolSize {
		newItem = rb.pool
		rb.poolSize = 0
		rb.pool = nil
	} else {
		newItem = rb.create(size - rb.poolSize).Link(rb.pool)
		rb.poolSize = 0
		rb.pool = nil
	}
	rb.Link(newItem)
	rb.Size += size
	return
}

func (rb *RingWriter[T, F]) Recycle(r *util.Ring[F]) {
	rb.poolSize++
	r.Value.Init()
	r.Value.Reset()
	if rb.pool == nil {
		rb.pool = r
	} else {
		rb.pool.Link(r)
	}
}

func (rb *RingWriter[T, F]) Reduce(size int) {
	r := rb.Unlink(size)
	if size > 1 {
		for p := r.Next(); p != r; {
			next := p.Next() //先保存下一个节点
			if p.Value.Discard() == 0 {
				rb.Recycle(p.Prev().Unlink(1))
			} else {
				// fmt.Println("Reduce", p.Value.ReaderCount())
			}
			p = next
		}
	}
	if r.Value.Discard() == 0 {
		rb.Recycle(r)
	}
	rb.Size -= size
	return
}

func (rb *RingWriter[T, F]) Step() (normal bool) {
	rb.LastValue.Broadcast() // 防止订阅者还在等待
	rb.LastValue = rb.Value
	nextSeq := rb.LastValue.GetSequence() + 1
	next := rb.Next()
	if normal = next.Value.StartWrite(); normal {
		next.Value.Reset()
		rb.Ring = next
	} else {
		rb.Reduce(1)         //抛弃还有订阅者的节点
		rb.Ring = rb.Glow(1) //补充一个新节点
		rb.Value.StartWrite()
	}
	rb.Value.SetSequence(nextSeq)
	rb.LastValue.Ready()
	return
}

func (rb *RingWriter[T, F]) GetReaderCount() int32 {
	return rb.ReaderCount.Load()
}