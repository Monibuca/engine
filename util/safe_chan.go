package util

import (
	"math"
	"sync/atomic"
)

// SafeChan安全的channel，可以防止close后被写入的问题
type SafeChan[T any] struct {
	C       chan T
	senders int32 //当前发送者数量
}


func (sc *SafeChan[T]) Init(n int) {
	sc.C = make(chan T, n)
}

// Close senders为0的时候可以安全关闭，否则不能关闭
func (sc *SafeChan[T]) Close() bool {
	if atomic.CompareAndSwapInt32(&sc.senders, 0, math.MinInt32) {
		close(sc.C)
		return true
	}
	return false
}

func (sc *SafeChan[T]) Send(v T) bool {
	// senders增加后为正数说明没有被channel没有被关闭，可以发送数据
	if atomic.AddInt32(&sc.senders, 1) > 0 {
		sc.C <- v
		atomic.AddInt32(&sc.senders, -1)
		return true
	}
	return false
}

func (sc *SafeChan[T]) IsClosed() bool {
	return atomic.LoadInt32(&sc.senders) < 0
}

func (sc *SafeChan[T]) IsEmpty() bool {
	return atomic.LoadInt32(&sc.senders) == 0
}

func (sc *SafeChan[T]) IsFull() bool {
	return atomic.LoadInt32(&sc.senders) > 0
}

type Promise[S any, R any] struct {
	Value S
	c     chan R
}

func (r *Promise[S, R]) Resolve(result R) {
	r.c <- result
}

func (r *Promise[S, R]) Then() R {
	return <-r.c
}

func NewPromise[S any, R any](value S) *Promise[S, R] {
	return &Promise[S, R]{
		Value: value,
		c:     make(chan R, 1),
	}
}
