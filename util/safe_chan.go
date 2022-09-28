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
	err   error
	c     chan R
	state int32 // 0 pendding  1 fullfilled -1 rejected
}

func (r *Promise[S, R]) Resolve(result R) {
	if atomic.CompareAndSwapInt32(&r.state, 0, 1) {
		r.c <- result
	}
}
func (r *Promise[S, R]) Reject(err error) {
	if atomic.CompareAndSwapInt32(&r.state, 0, -1) {
		r.err = err
		close(r.c)
	}
}
func (p *Promise[S, R]) AWait() (R, error) {
	return <-p.c, p.err
}
func (p *Promise[S, R]) Then() (r R) {
	r, _ = p.AWait()
	return
}
func (p *Promise[S, R]) Catch() (err error) {
	_, err = p.AWait()
	return
}
func NewPromise[S any, R any](value S) *Promise[S, R] {
	return &Promise[S, R]{
		Value: value,
		c:     make(chan R, 1),
	}
}
