package engine

import (
	"container/ring"
	"context"
	"reflect"
	"sync"
	"sync/atomic"
	"time"
)

type DataItem struct {
	Timestamp time.Time
	Sequence  int
	Value     interface{}
}

// TODO: 池化，泛型

type LockItem struct {
	DataItem
	sync.RWMutex
}

type RingBuffer struct {
	*ring.Ring
	Size int
	Flag *int32
	context.Context
}

func (rb *RingBuffer) Init(ctx context.Context, n int) *RingBuffer {
	var flag int32
	if rb == nil {
		rb = &RingBuffer{Context: ctx, Ring: ring.New(n), Flag: &flag}
	} else {
		rb.Ring = ring.New(n)
		rb.Size = n
		rb.Context = ctx
		rb.Flag = &flag
	}
	for x := rb.Ring; x.Value == nil; x = x.Next() {
		x.Value = new(LockItem)
	}
	rb.Current().Lock()
	return rb
}

func (rb RingBuffer) Clone() *RingBuffer {
	return &rb
}

func (rb RingBuffer) SubRing(rr *ring.Ring) *RingBuffer {
	rb.Ring = rr
	return &rb
}

func (rb *RingBuffer) CurrentValue() interface{} {
	return rb.Current().Value
}

func (rb *RingBuffer) NextValue() interface{} {
	return rb.Next().Value.(*LockItem).Value
}

func (rb *RingBuffer) Current() *LockItem {
	return rb.Ring.Value.(*LockItem)
}

func (rb *RingBuffer) MoveNext() {
	rb.Ring = rb.Next()
}

func (rb *RingBuffer) GetNext() *LockItem {
	rb.MoveNext()
	return rb.Current()
}

func (rb *RingBuffer) Read() interface{} {
	current := rb.Current()
	current.RLock()
	defer current.RUnlock()
	return current.Value
}

func (rb *RingBuffer) Step() {
	last := rb.Current()
	if atomic.CompareAndSwapInt32(rb.Flag, 0, 1) {
		current := rb.GetNext()
		current.Lock()
		last.Unlock()
		//Flag不为1代表被Dispose了，但尚未处理Done
		if !atomic.CompareAndSwapInt32(rb.Flag, 1, 0) {
			current.Unlock()
		}
	}
}

func (rb *RingBuffer) Write(value interface{}) {
	last := rb.Current()
	last.Value = value
	if atomic.CompareAndSwapInt32(rb.Flag, 0, 1) {
		current := rb.GetNext()
		current.Lock()
		last.Unlock()
		//Flag不为1代表被Dispose了，但尚未处理Done
		if !atomic.CompareAndSwapInt32(rb.Flag, 1, 0) {
			current.Unlock()
		}
	}
}

func (rb *RingBuffer) Dispose() {
	current := rb.Current()
	if atomic.CompareAndSwapInt32(rb.Flag, 0, 2) {
		current.Unlock()
	} else if atomic.CompareAndSwapInt32(rb.Flag, 1, 2) {
		//当前是1代表正在写入，此时变成2，但是Done的任务得交给NextW来处理
	} else if atomic.CompareAndSwapInt32(rb.Flag, 0, 2) {
		current.Unlock()
	}
}

func (rb *RingBuffer) read() reflect.Value {
	return reflect.ValueOf(rb.Read())
}

func (rb *RingBuffer) nextRead() reflect.Value {
	rb.MoveNext()
	return rb.read()
}

// ReadLoop 循环读取，采用了反射机制，不适用高性能场景
// handler入参可以传入回调函数或者channel
func (rb *RingBuffer) ReadLoop(handler interface{}) {
	rb.ReadLoopConditional(handler, func() bool {
		return rb.Err() == nil && *rb.Flag != 2
	})
}

// goon判断函数用来判断是否继续读取,返回false将终止循环
func (rb *RingBuffer) ReadLoopConditional(handler interface{}, goon func() bool) {
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
