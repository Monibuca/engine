package common

import (
	"context"
	"reflect"
	"sync"
	"sync/atomic"
)

type LockFrame[T any] struct {
	DataFrame[T]
	sync.RWMutex
}

type LockRing[T any] struct {
	RingBuffer[LockFrame[T]]
	ctx context.Context
	Flag *int32
}

func (lr *LockRing[T]) Init(ctx context.Context, n int) *LockRing[T] {
	var flag int32
	if lr == nil {
		lr = &LockRing[T]{}
	}
	lr.ctx = ctx
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

func (rb *LockRing[T]) read() reflect.Value {
	return reflect.ValueOf(rb.Read())
}

func (rb *LockRing[T]) nextRead() reflect.Value {
	rb.MoveNext()
	return rb.read()
}

func (rb *LockRing[T]) condition() bool {
	return rb.ctx.Err() == nil && *rb.Flag != 2
}

// ReadLoop 循环读取，采用了反射机制，不适用高性能场景
// handler入参可以传入回调函数或者channel
func (rb *LockRing[T]) ReadLoop(handler interface{}, async bool) {
	if async {
		rb.ReadLoopConditionalGo(handler, rb.condition)
	} else {
		rb.ReadLoopConditional(handler, rb.condition)
	}
}

// goon判断函数用来判断是否继续读取,返回false将终止循环
func (rb *LockRing[T]) ReadLoopConditional(handler interface{}, goon func() bool) {
	switch t := reflect.ValueOf(handler); t.Kind() {
	case reflect.Chan:
		for v := rb.read(); goon(); v = rb.nextRead() {
			t.Send(v)
		}
	case reflect.Func:
		for args := []reflect.Value{rb.read()}; goon(); args[0] = rb.nextRead() {
			t.Call(args)
		}
	}
}

// goon判断函数用来判断是否继续读取,返回false将终止循环
func (r *LockRing[T]) ReadLoopConditionalGo(handler interface{}, goon func() bool) {
	switch t := reflect.ValueOf(handler); t.Kind() {
	case reflect.Chan:
		for v := r.read(); goon(); v = r.nextRead() {
			t.Send(v)
		}
	case reflect.Func:
		for args := []reflect.Value{r.read()}; goon(); args[0] = r.nextRead() {
			go t.Call(args)
		}
	}
}
