package util

import (
	"context"
	"errors"
	"math"
	"sync/atomic"
	"time"
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

var errResolved = errors.New("resolved")

type Promise[S any] struct {
	context.Context
	context.CancelCauseFunc
	context.CancelFunc
	Value S
}

func (r *Promise[S]) Resolve() {
	r.CancelCauseFunc(errResolved)
}

func (r *Promise[S]) Reject(err error) {
	r.CancelCauseFunc(err)
}

func (p *Promise[S]) Await() (err error) {
	<-p.Done()
	err = context.Cause(p.Context)
	if err == errResolved {
		err = nil
	}
	p.CancelFunc()
	return
}

func NewPromise[S any](value S) *Promise[S] {
	ctx0, cancel0 := context.WithTimeout(context.Background(), time.Second*10)
	ctx, cancel := context.WithCancelCause(ctx0)
	return &Promise[S]{
		Value:           value,
		Context:         ctx,
		CancelCauseFunc: cancel,
		CancelFunc:      cancel0,
	}
}
