package common

import (
	"m7s.live/engine/v4/util"
)

type RingBuffer[T any] struct {
	*util.Ring[T] `json:"-"`
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