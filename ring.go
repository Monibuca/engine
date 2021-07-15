package engine

import (
	"container/ring"
	"reflect"
	"sync"
	// "time"
)

type RingItem struct {
	Value interface{}
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

func (r *RingBuffer) GetNext() *RingItem {
	r.Ring = r.Next()
	return r.Current()
}

func (r *RingBuffer) Read() interface{} {
	current := r.Current()
	current.Wait()
	return current.Value
}

func (r *RingBuffer) ReadLoop(handler interface{}, goon func() bool) {
	if goon == nil {
		switch t := reflect.ValueOf(handler); t.Kind() {
		case reflect.Chan:
			for v := r.read(); ; v = r.read() {
				t.Send(v)
				r.MoveNext()
			}
		case reflect.Func:
			for args := []reflect.Value{r.read()}; ; args[0] = r.read() {
				t.Call(args)
				r.MoveNext()
			}
		}
	} else {
		switch t := reflect.ValueOf(handler); t.Kind() {
		case reflect.Chan:
			for v := r.read(); goon(); v = r.read() {
				t.Send(v)
				r.MoveNext()
			}
		case reflect.Func:
			for args := []reflect.Value{r.read()}; goon(); args[0] = r.read() {
				t.Call(args)
				r.MoveNext()
			}
		}
	}
}
