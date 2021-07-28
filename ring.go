package engine

import (
	"container/ring"
	"context"
	"reflect"
	"sync"
	"sync/atomic"
)

// TODO: 池化，泛型

type LockItem struct {
	Value interface{}
	sync.RWMutex
}

type RingBuffer struct {
	*ring.Ring
	done uint32
	context.Context
}

func (r *RingBuffer) Init(ctx context.Context, n int) *RingBuffer {
	if r == nil {
		r = &RingBuffer{Context: ctx, Ring: ring.New(n)}
	} else {
		r.Ring = ring.New(n)
		r.Context = ctx
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

func (r *RingBuffer) Write(value interface{}) {
	last := r.Current()
	last.Value = value
	r.GetNext().Lock()
	last.Unlock()
}

func (r *RingBuffer) Dispose() {
	atomic.StoreUint32(&r.done,1) 
	r.Current().Unlock()
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
		return r.Err() == nil
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
