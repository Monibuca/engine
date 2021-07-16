package engine

import (
	"container/ring"
	"sync/atomic"
)

// 可释放的Ring，用于音视频
type RingDisposable struct {
	RingBuffer
	Flag *int32 // 0:不在写入，1：正在写入，2：已销毁
}

func (rb *RingDisposable) Init(n int) {
	var flag int32
	rb.RingBuffer.Init(n)
	rb.Flag = &flag
}

func (rb RingDisposable) Clone() *RingDisposable {
	return &rb
}

func (r RingDisposable) SubRing(rr *ring.Ring) *RingDisposable {
	r.Ring = rr
	return &r
}

func (r *RingDisposable) Write(value interface{}) {
	last := r.Current()
	last.Value = value
	if atomic.CompareAndSwapInt32(r.Flag, 0, 1) {
		current := r.GetNext()
		current.Add(1)
		last.Done()
		//Flag不为1代表被Dispose了，但尚未处理Done
		if !atomic.CompareAndSwapInt32(r.Flag, 1, 0) {
			current.Done()
		}
	}
}

func (r *RingDisposable) Step() {
	last := r.Current()
	if atomic.CompareAndSwapInt32(r.Flag, 0, 1) {
		current := r.GetNext()
		current.Add(1)
		last.Done()
		//Flag不为1代表被Dispose了，但尚未处理Done
		if !atomic.CompareAndSwapInt32(r.Flag, 1, 0) {
			current.Done()
		}
	}
}
func (r *RingDisposable) Dispose() {
	current := r.Current()
	if atomic.CompareAndSwapInt32(r.Flag, 0, 2) {
		current.Done()
	} else if atomic.CompareAndSwapInt32(r.Flag, 1, 2) {
		//当前是1代表正在写入，此时变成2，但是Done的任务得交给NextW来处理
	} else if atomic.CompareAndSwapInt32(r.Flag, 0, 2) {
		current.Done()
	}
}
// Goon 是否继续
func (r *RingDisposable) Goon() bool {
	return *r.Flag != 2
}

func (r *RingDisposable) ReadLoop(handler interface{}) {
	r.RingBuffer.ReadLoopConditional(handler, r.Goon)
}