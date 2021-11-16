package engine

import (
	"container/ring"
	"context"
	"runtime"
	"time"
)

type AVItem struct {
	DataItem
	canRead bool
}

type AVRing struct {
	RingBuffer
	poll time.Duration
}

func (r *AVRing) Init(ctx context.Context, n int) *AVRing {
	r.Ring = ring.New(n)
	r.Context = ctx
	r.Size = n
	for x := r.Ring; x.Value == nil; x = x.Next() {
		x.Value = new(AVItem)
	}
	return r
}
func (r AVRing) Clone() *AVRing {
	return &r
}

func (r AVRing) SubRing(rr *ring.Ring) *AVRing {
	r.Ring = rr
	return &r
}
func (r *AVRing) Write(value interface{}) {
	last := r.Current()
	last.Value = value
	r.GetNext().canRead = false
	last.canRead = true
}

func (r *AVRing) Step() {
	last := r.Current()
	r.GetNext().canRead = false
	last.canRead = true
}

func (r *AVRing) wait() {
	if r.poll == 0 {
		runtime.Gosched()
	} else {
		time.Sleep(r.poll)
	}
}

func (r *AVRing) CurrentValue() interface{} {
	return r.Current().Value
}

func (r *AVRing) Current() *AVItem {
	return r.Ring.Value.(*AVItem)
}

func (r *AVRing) NextValue() interface{} {
	return r.Next().Value.(*AVItem).Value
}
func (r *AVRing) PreItem() *AVItem {
	return r.Prev().Value.(*AVItem)
}
func (r *AVRing) GetNext() *AVItem {
	r.MoveNext()
	return r.Current()
}

func (r *AVRing) Read() (item *AVItem, value interface{}) {
	current := r.Current()
	for r.Err() == nil && !current.canRead {
		r.wait()
	}
	return current, current.Value
}

func (r *AVRing) TryRead() (item *AVItem, value interface{}) {
	current := r.Current()
	if r.Err() == nil && !current.canRead {
		return nil, nil
	}
	return current, current.Value
}

// 定位到给定时刻之后的第一个位置
func (r AVRing) Location(t time.Time) *AVRing {
	for r.PreItem().canRead && r.PreItem().Timestamp.After(t) {
		r.Ring = r.Prev()
	}
	return &r
}
