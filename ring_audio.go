package engine

import (
	"bytes"
	"sync"
	"time"
)

type RingItem_Audio struct {
	AudioPack
	sync.WaitGroup
	*bytes.Buffer
	UpdateTime time.Time
}

// Ring 环形缓冲，使用数组实现
type Ring_Audio struct {
	Current *RingItem_Audio
	buffer []RingItem_Audio
	Index  byte
}
func (r *Ring_Audio) SubRing(index byte) *Ring_Audio{
	result:= &Ring_Audio{
		buffer:r.buffer,
	}
	result.GoTo(index)
	return result
}
// NewRing 创建Ring
func NewRing_Audio() (r *Ring_Audio) {
	r = &Ring_Audio{
		buffer : make([]RingItem_Audio, 256),
	}
	r.GoTo(0)
	r.Current.Add(1)
	return
}
func (r *Ring_Audio) offset(v byte) byte {
	return r.Index + v
}

// GoTo 移动到指定索引处
func (r *Ring_Audio) GoTo(index byte) {
	r.Index = index
	r.Current = &r.buffer[index]
}

// GetAt 获取指定索引处的引用
func (r *Ring_Audio) GetAt(index byte) *RingItem_Audio {
	return &r.buffer[index]
}

// GetNext 获取下一个位置的引用
func (r *Ring_Audio) GetNext() *RingItem_Audio {
	return &r.buffer[r.Index+1]
}

// GetLast 获取上一个位置的引用
func (r *Ring_Audio) GetLast() *RingItem_Audio {
	return &r.buffer[r.Index-1]
}

// GoNext 移动到下一个位置
func (r *Ring_Audio) GoNext() {
	r.Index = r.Index+1
	r.Current = &r.buffer[r.Index]
}

// GoBack 移动到上一个位置
func (r *Ring_Audio) GoBack() {
	r.Index = r.Index-1
	r.Current = &r.buffer[r.Index]
}

// NextW 写下一个
func (r *Ring_Audio) NextW() {
	item := r.Current
	item.UpdateTime = time.Now()
	r.GoNext()
	r.Current.Add(1)
	item.Done()
}

// NextR 读下一个
func (r *Ring_Audio) NextR(){
	r.GoNext()
	r.Current.Wait()
}

func (r *Ring_Audio) GetBuffer() *bytes.Buffer {
	if r.Current.Buffer == nil {
		r.Current.Buffer = bytes.NewBuffer([]byte{})
	} else {
		r.Current.Reset()
	}
	return r.Current.Buffer
}

// Timeout 发布者是否超时了
func (r *Ring_Audio) Timeout(t time.Duration) bool {
	// 如果设置为0则表示永不超时
	if t==0 {
		return false
	}
	return time.Since(r.Current.UpdateTime) >t
}
