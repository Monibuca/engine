package common

import (
	"m7s.live/engine/v4/util"
)

type RingBuffer[T any] struct {
	*util.Ring[T] `json:"-" yaml:"-"`
	Size          int
	MoveCount     uint32
	LastValue     *T
}

func (rb *RingBuffer[T]) Init(n int) *RingBuffer[T] {
	if rb == nil {
		rb = new(RingBuffer[T])
	}
	rb.Ring = util.NewRing[T](n)
	rb.Size = n
	rb.LastValue = &rb.Value
	return rb
}

func (rb *RingBuffer[T]) MoveNext() *T {
	rb.LastValue = &rb.Value
	rb.Ring = rb.Next()
	rb.MoveCount++
	return &rb.Value
}

func (rb *RingBuffer[T]) Glow(size int) (newItem *util.Ring[T]) {
	newItem = rb.Link(util.NewRing[T](size))
	rb.Size += size
	return
}

func (rb *RingBuffer[T]) Reduce(size int) (newItem *RingBuffer[T]) {
	newItem = &RingBuffer[T]{
		Ring: rb.Unlink(size),
		Size: size,
	}
	rb.Size -= size
	return
}

// Do calls function f on each element of the ring, in forward order.
// The behavior of Do is undefined if f changes *r.
func (rb *RingBuffer[T]) Do(f func(*T)) {
	if rb != nil {
		f(&rb.Value)
		for p := rb.Next(); p != rb.Ring; p = p.Next() {
			f(&p.Value)
		}
	}
}
