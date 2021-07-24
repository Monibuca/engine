package engine

import (
	"container/ring"
	"reflect"
	"sync"
	"sync/atomic"
	"time"
	// "time"
)

type RingItem struct {
	Value   interface{}
	reading int32
	sync.WaitGroup
}

type RingBuffer struct {
	*ring.Ring
}

// TODO: 池化，泛型

func NewRingBuffer(n int) (r *RingBuffer) {
	r = new(RingBuffer)
	r.Init(n)
	return
}

func (r *RingBuffer) Init(n int) {
	r.Ring = ring.New(n)
	for x := r.Ring; x.Value == nil; x = x.Next() {
		x.Value = new(RingItem)
	}
	r.Current().Add(1)
}

func (rb RingBuffer) Clone() *RingBuffer {
	return &rb
}

func (r RingBuffer) SubRing(rr *ring.Ring) *RingBuffer {
	r.Ring = rr
	return &r
}

func (r *RingBuffer) Write(value interface{}) {
	last := r.Current()
	last.Value = value
	r.GetNext().Add(1)
	last.Done()
}

func (r *RingBuffer) read() reflect.Value {
	current := r.Current()
	current.Wait()
	return reflect.ValueOf(current.Value)
}

func (r *RingBuffer) nextRead() reflect.Value {
	r.MoveNext()
	return r.read()
}

func (r *RingBuffer) CurrentValue() interface{} {
	return r.Current().Value
}

func (r *RingBuffer) NextValue() interface{} {
	return r.Next().Value.(*RingItem).Value
}

func (r *RingBuffer) Current() *RingItem {
	return r.Ring.Value.(*RingItem)
}

func (r *RingBuffer) MoveNext() {
	r.Ring = r.Next()
}

func (r *RingBuffer) NextRead() interface{} {
	r.MoveNext()
	return r.Read()
}

func (r *RingBuffer) GetNext() *RingItem {
	r.MoveNext()
	return r.Current()
}

func (r *RingBuffer) Read() interface{} {
	current := r.Current()
	atomic.AddInt32(&current.reading, 1)
	current.Wait()
	atomic.AddInt32(&current.reading, -1)
	return current.Value
}

// ReadLoop 循环读取，采用了反射机制，不适用高性能场景
// handler入参可以传入回调函数或者channel
func (r *RingBuffer) ReadLoop(handler interface{}) {
	switch t := reflect.ValueOf(handler); t.Kind() {
	case reflect.Chan:
		for v := r.read(); ; v = r.nextRead() {
			t.Send(v)
		}
	case reflect.Func:
		for args := []reflect.Value{r.read()}; ; args[0] = r.nextRead() {
			t.Call(args)
		}
	}
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
