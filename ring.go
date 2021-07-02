package engine

import (
	"container/ring"
	"sync"
	"sync/atomic"
	// "time"
)

type RingItem struct {
	Value interface{}
	sync.WaitGroup
}

type RingBuffer struct {
	*ring.Ring
	// UpdateTime time.Time //更新时间，用于计算是否超时
}

// 可释放的Ring，用于音视频
type RingDisposable struct {
	RingBuffer
	Flag int32 // 0:不在写入，1：正在写入，2：已销毁
}

// 带锁的Ring，用于Hook
// type RingLock struct {
// 	RingBuffer
// 	sync.Mutex
// }
// func (r *RingLock) Write(value interface{}) {
// 	r.Lock()
// 	r.RingBuffer.Write(value)
// 	r.Unlock()
// }

// TODO: 池化，泛型

func NewRingBuffer(n int) (r *RingBuffer) {
	r = new(RingBuffer)
	r.Init(n)
	return
}

func (r *RingBuffer) Init(n int) {
	r.Ring = ring.New(n)
	// r.UpdateTime = time.Now()
	for x := r.Ring; x.Value == nil; x = x.Next() {
		x.Value = new(RingItem)
	}
}

func (rb RingBuffer) Clone() *RingBuffer {
	return &rb
}

func (r *RingBuffer) SubRing(rr *ring.Ring) *RingBuffer {
	r = r.Clone()
	r.Ring = rr
	return r
}

func (r *RingBuffer) Write(value interface{}) {
	// r.UpdateTime = time.Now()
	last := r.Current()
	last.Value = value
	r.GetNext().Add(1)
	last.Done()
}

func (r *RingDisposable) Write(value interface{}) {
	// r.UpdateTime = time.Now()
	last := r.Current()
	last.Value = value
	if atomic.CompareAndSwapInt32(&r.Flag, 0, 1) {
		current := r.GetNext()
		current.Add(1)
		last.Done()
		//Flag不为1代表被Dispose了，但尚未处理Done
		if !atomic.CompareAndSwapInt32(&r.Flag, 1, 0) {
			current.Done()
		}
	}
}

func (r *RingBuffer) Read() interface{} {
	current := r.Current()
	current.Wait()
	return current.Value
}

func (r *RingBuffer) CurrentValue() interface{} {
	return r.Current().Value
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

func (r *RingDisposable) Dispose() {
	current := r.Current()
	if atomic.CompareAndSwapInt32(&r.Flag, 0, 2) {
		current.Done()
	} else if atomic.CompareAndSwapInt32(&r.Flag, 1, 2) {
		//当前是1代表正在写入，此时变成2，但是Done的任务得交给NextW来处理
	} else if atomic.CompareAndSwapInt32(&r.Flag, 0, 2) {
		current.Done()
	}
}

// // Timeout 发布者是否超时了
// func (r *RingBuffer) Timeout(t time.Duration) bool {
// 	// 如果设置为0则表示永不超时
// 	if t == 0 {
// 		return false
// 	}
// 	return time.Since(r.UpdateTime) > t
// }
