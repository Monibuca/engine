package common

import (
	"m7s.live/engine/v4/util"
)

type RingBuffer[T any] struct {
	*util.Ring[T]
	Size      int
	MoveCount uint32
}

func (rb *RingBuffer[T]) Init(n int) *RingBuffer[T] {
	if rb == nil {
		rb = new(RingBuffer[T])
	}
	rb.Ring = util.NewRing[T](n)
	rb.Size = n
	return rb
}

func (rb RingBuffer[T]) SubRing(rr *util.Ring[T]) *RingBuffer[T] {
	rb.Ring = rr
	rb.MoveCount = 0
	return &rb
}

func (rb *RingBuffer[T]) MoveNext() *T {
	rb.Ring = rb.Next()
	rb.MoveCount++
	return &rb.Value
}

func (rb *RingBuffer[T]) PreValue() *T {
	return &rb.Prev().Value
}
