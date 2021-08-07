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
	Value interface{}
}

// TODO: 池化，泛型

type LockItem struct {
	DataItem
	sync.RWMutex
}

type RingBuffer struct {
	*ring.Ring
	Flag *int32
	context.Context
}

func (r *RingBuffer) Init(ctx context.Context, n int) *RingBuffer {
	var flag int32
	if r == nil {
		r = &RingBuffer{Context: ctx, Ring: ring.New(n), Flag: &flag}
	} else {
		r.Ring = ring.New(n)
		r.Context = ctx
		r.Flag = &flag
	}
	for x := r.Ring; x.Value == nil; x = x.Next() {
		x.Value = new(LockItem)
	}
	r.Current().Lock()
	return r
}

func (rb RingBuffer) Clone() *RingBuffer {
	return &rb
}

func (r RingBuffer) SubRing(rr *ring.Ring) *RingBuffer {
	r.Ring = rr
	return &r
}

func (r *RingBuffer) CurrentValue() interface{} {
	return r.Current().Value
}

func (r *RingBuffer) NextValue() interface{} {
	return r.Next().Value.(*LockItem).Value
}

func (r *RingBuffer) Current() *LockItem {
	return r.Ring.Value.(*LockItem)
}

func (r *RingBuffer) MoveNext() {
	r.Ring = r.Next()
}

func (r *RingBuffer) GetNext() *LockItem {
	r.MoveNext()
	return r.Current()
}

func (r *RingBuffer) Read() interface{} {
	current := r.Current()
	current.RLock()
	defer current.RUnlock()
	return current.Value
}

func (r *RingBuffer) Step() {
	last := r.Current()
	if atomic.CompareAndSwapInt32(r.Flag, 0, 1) {
		current := r.GetNext()
		current.Lock()
		last.Unlock()
		//Flag不为1代表被Dispose了，但尚未处理Done
		if !atomic.CompareAndSwapInt32(r.Flag, 1, 0) {
			current.Unlock()
		}
	}
}

func (r *RingBuffer) Write(value interface{}) {
	last := r.Current()
	last.Value = value
	if atomic.CompareAndSwapInt32(r.Flag, 0, 1) {
		current := r.GetNext()
		current.Lock()
		last.Unlock()
		//Flag不为1代表被Dispose了，但尚未处理Done
		if !atomic.CompareAndSwapInt32(r.Flag, 1, 0) {
			current.Unlock()
		}
	}
}

func (r *RingBuffer) Dispose() {
	current := r.Current()
	if atomic.CompareAndSwapInt32(r.Flag, 0, 2) {
		current.Unlock()
	} else if atomic.CompareAndSwapInt32(r.Flag, 1, 2) {
		//当前是1代表正在写入，此时变成2，但是Done的任务得交给NextW来处理
	} else if atomic.CompareAndSwapInt32(r.Flag, 0, 2) {
		current.Unlock()
	}
}

func (r *RingBuffer) read() reflect.Value {
	return reflect.ValueOf(r.Read())
}

func (r *RingBuffer) nextRead() reflect.Value {
	r.MoveNext()
	return r.read()
}

// ReadLoop 循环读取，采用了反射机制，不适用高性能场景
// handler入参可以传入回调函数或者channel
func (r *RingBuffer) ReadLoop(handler interface{}) {
	r.ReadLoopConditional(handler, func() bool {
		return r.Err() == nil && *r.Flag != 2
	})
}

// goon判断函数用来判断是否继续读取,返回false将终止循环
func (r *RingBuffer) ReadLoopConditional(handler interface{}, goon func() bool) {
	switch t := reflect.ValueOf(handler); t.Kind() {
	case reflect.Chan:
		for v := r.read(); goon(); v = r.nextRead() {
			t.Send(v)
		}
	case reflect.Func:
		for args := []reflect.Value{r.read()}; goon(); args[0] = r.nextRead() {
			t.Call(args)
		}
	}
}
