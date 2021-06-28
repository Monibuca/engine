package engine

import (
	"context"
	"sync"
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
		if hooks.Current.Wait(); name == hooks.Current.Name {
			go callback(hooks.Current.Payload)
		}
	}
}

func AddHookWithContext(ctx context.Context, name string, callback func(interface{})) {
	for hooks := Hooks.SubRing(Hooks.Index); ctx.Err() == nil; hooks.GoNext() {
		if hooks.Current.Wait(); name == hooks.Current.Name && ctx.Err() == nil {
			go callback(hooks.Current.Payload)
		}
	}
}

func TriggerHook(hook Hook) {
	hookMutex.Lock()
	Hooks.NextW(hook)
	hookMutex.Unlock()
}

type RingItem_Hook struct {
	Hook
	sync.WaitGroup
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
	buffer := make([]RingItem_Hook, 256)
	r = &Ring_Hook{
		buffer:  buffer,
		Current: &buffer[0],
	}
	r.Current.Add(1)
	return
}

// GoTo 移动到指定索引处
func (r *Ring_Hook) GoTo(index byte) {
	r.Index = index
	r.Current = &r.buffer[index]
}

// GoNext 移动到下一个位置
func (r *Ring_Hook) GoNext() {
	r.Index = r.Index + 1
	r.Current = &r.buffer[r.Index]
}

// NextW 写下一个
func (r *Ring_Hook) NextW(hook Hook) {
	item := r.Current
	item.Hook = hook
	r.GoNext()
	r.Current.Add(1)
	item.Done()
}

// NextR 读下一个
func (r *Ring_Hook) NextR() {
	r.Current.Wait()
	r.GoNext()
}
