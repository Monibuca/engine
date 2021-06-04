package engine

import (
	"bytes"
	"context"
	"sync"
	"time"
)

type Hook struct {
	Name    string
	Payload interface{}
}
type TransCodeReq struct {
	*Subscriber
	RequestCodec string
}

const (
	HOOK_SUBSCRIBE          = "Subscribe"
	HOOK_UNSUBSCRIBE        = "UnSubscibe"
	HOOK_STREAMCLOSE        = "StreamClose"
	HOOK_PUBLISH            = "Publish"
	HOOK_REQUEST_TRANSAUDIO = "RequestTransAudio"
)

var Hooks = NewRing_Hook()
var hookMutex sync.Mutex
func AddHook(name string, callback func(interface{})) {
	for hooks := Hooks.SubRing(Hooks.Index); ; hooks.GoNext() {
		hooks.Current.Wait()
		if name == hooks.Current.Name {
			go callback(hooks.Current.Payload)
		}
	}
}

func AddHookWithContext(ctx context.Context, name string, callback func(interface{})) {
	for hooks := Hooks.SubRing(Hooks.Index); ctx.Err() == nil; hooks.GoNext() {
		hooks.Current.Wait()
		if name == hooks.Current.Name && ctx.Err() == nil {
			go callback(hooks.Current.Payload)
		}
	}
}

func TriggerHook(hook Hook) {
	Hooks.Current.Hook = hook
	hookMutex.Lock()
	Hooks.NextW()
	hookMutex.Unlock()
}

type RingItem_Hook struct {
	Hook
	sync.WaitGroup
	*bytes.Buffer
	UpdateTime time.Time
}

// Ring 环形缓冲，使用数组实现
type Ring_Hook struct {
	Current *RingItem_Hook
	buffer  []RingItem_Hook
	Index   byte
}

func (r *Ring_Hook) SubRing(index byte) *Ring_Hook {
	result := &Ring_Hook{
		buffer: r.buffer,
	}
	result.GoTo(index)
	return result
}

// NewRing 创建Ring
func NewRing_Hook() (r *Ring_Hook) {
	r = &Ring_Hook{
		buffer: make([]RingItem_Hook, 256),
	}
	r.GoTo(0)
	r.Current.Add(1)
	return
}

// GoTo 移动到指定索引处
func (r *Ring_Hook) GoTo(index byte) {
	r.Index = index
	r.Current = &r.buffer[index]
}

// GetAt 获取指定索引处的引用
func (r *Ring_Hook) GetAt(index byte) *RingItem_Hook {
	return &r.buffer[index]
}

// GetNext 获取下一个位置的引用
func (r *Ring_Hook) GetNext() *RingItem_Hook {
	return &r.buffer[r.Index+1]
}

// GetLast 获取上一个位置的引用
func (r *Ring_Hook) GetLast() *RingItem_Hook {
	return &r.buffer[r.Index-1]
}

// GoNext 移动到下一个位置
func (r *Ring_Hook) GoNext() {
	r.Index = r.Index + 1
	r.Current = &r.buffer[r.Index]
}

// GoBack 移动到上一个位置
func (r *Ring_Hook) GoBack() {
	r.Index = r.Index - 1
	r.Current = &r.buffer[r.Index]
}

// NextW 写下一个
func (r *Ring_Hook) NextW() {
	item := r.Current
	item.UpdateTime = time.Now()
	r.GoNext()
	r.Current.Add(1)
	item.Done()
}

// NextR 读下一个
func (r *Ring_Hook) NextR() {
	r.Current.Wait()
	r.GoNext()
}

func (r *Ring_Hook) GetBuffer() *bytes.Buffer {
	if r.Current.Buffer == nil {
		r.Current.Buffer = bytes.NewBuffer([]byte{})
	} else {
		r.Current.Reset()
	}
	return r.Current.Buffer
}

// Timeout 发布者是否超时了
func (r *Ring_Hook) Timeout(t time.Duration) bool {
	// 如果设置为0则表示永不超时
	if t == 0 {
		return false
	}
	return time.Since(r.Current.UpdateTime) > t
}
