package engine

import (
	"bytes"
	"sync"
	"time"
)

type RingItem_Video struct {
	VideoPack
	sync.WaitGroup
	*bytes.Buffer
	UpdateTime time.Time
}

// Ring 环形缓冲，使用数组实现
type Ring_Video struct {
	Current *RingItem_Video
	buffer []RingItem_Video
	Index  byte
}
func (r *Ring_Video) SubRing(index byte) *Ring_Video{
	result:= &Ring_Video{
		buffer:r.buffer,
	}
	result.GoTo(index)
	return result
}
// NewRing 创建Ring
func NewRing_Video() (r *Ring_Video) {
	r = &Ring_Video{
		buffer : make([]RingItem_Video, 256),
	}
	r.GoTo(0)
	r.Current.Add(1)
	return
}

// GoTo 移动到指定索引处
func (r *Ring_Video) GoTo(index byte) {
	r.Index = index
	r.Current = &r.buffer[index]
}

// GetAt 获取指定索引处的引用
func (r *Ring_Video) GetAt(index byte) *RingItem_Video {
	return &r.buffer[index]
}

// GetNext 获取下一个位置的引用
func (r *Ring_Video) GetNext() *RingItem_Video {
	return &r.buffer[r.Index+1]
}

// GetLast 获取上一个位置的引用
func (r *Ring_Video) GetLast() *RingItem_Video {
	return &r.buffer[r.Index-1]
}

// GoNext 移动到下一个位置
func (r *Ring_Video) GoNext() {
	r.Index = r.Index+1
	r.Current = &r.buffer[r.Index]
}

// GoBack 移动到上一个位置
func (r *Ring_Video) GoBack() {
	r.Index = r.Index-1
	r.Current = &r.buffer[r.Index]
}

// NextW 写下一个
func (r *Ring_Video) NextW() {
	item := r.Current
	item.UpdateTime = time.Now()
	r.GoNext()
	r.Current.Add(1)
	item.Done()
}

// NextR 读下一个
func (r *Ring_Video) NextR(){
	r.GoNext()
	r.Current.Wait()
}

func (r *Ring_Video) GetBuffer() *bytes.Buffer {
	if r.Current.Buffer == nil {
		r.Current.Buffer = bytes.NewBuffer([]byte{})
	} else {
		r.Current.Reset()
	}
	return r.Current.Buffer
}

// Timeout 发布者是否超时了
func (r *Ring_Video) Timeout(t time.Duration) bool {
	// 如果设置为0则表示永不超时
	if t==0 {
		return false
	}
	return time.Since(r.Current.UpdateTime) >t
}
