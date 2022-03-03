package common

import (
	"sync"
	"sync/atomic"
)

type LockFrame[T any] struct {
	DataFrame[T]
	sync.RWMutex
}

type LockRing[T any] struct {
	RingBuffer[LockFrame[T]]
	Flag *int32
}

func (lr *LockRing[T]) Init(n int) *LockRing[T] {
	var flag int32
	if lr == nil {
		lr = &LockRing[T]{}
	}
	lr.RingBuffer.Init(n)
	lr.Flag = &flag
	lr.Value.Lock()
	return lr
}

func (rb *LockRing[T]) Read() *DataFrame[T] {
	current := rb.Value
	current.RLock()
	defer current.RUnlock()
	return &current.DataFrame
}

func (rb *LockRing[T]) Step() {
	last := &rb.Value
	if atomic.CompareAndSwapInt32(rb.Flag, 0, 1) {
		current := rb.MoveNext()
		current.Lock()
		last.Unlock()
		//Flag不为1代表被Dispose了，但尚未处理Done
		if !atomic.CompareAndSwapInt32(rb.Flag, 1, 0) {
			current.Unlock()
		}
	}
}

func (rb *LockRing[T]) Write(value T) {
	last := &rb.Value
	last.Value = value
	if atomic.CompareAndSwapInt32(rb.Flag, 0, 1) {
		current := rb.MoveNext()
		current.Lock()
		last.Unlock()
		//Flag不为1代表被Dispose了，但尚未处理Done
		if !atomic.CompareAndSwapInt32(rb.Flag, 1, 0) {
			current.Unlock()
		}
	}
}

func (rb *LockRing[T]) Dispose() {
	current := &rb.Value
	if atomic.CompareAndSwapInt32(rb.Flag, 0, 2) {
		current.Unlock()
	} else if atomic.CompareAndSwapInt32(rb.Flag, 1, 2) {
		//当前是1代表正在写入，此时变成2，但是Done的任务得交给NextW来处理
	} else if atomic.CompareAndSwapInt32(rb.Flag, 0, 2) {
		current.Unlock()
	}
}
