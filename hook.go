package engine

import (
	"context"
	"sync"
)
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

var Hooks = make(map[string]*RingBuffer)
var hookLocker sync.Mutex

func AddHook(name string, callback func(interface{})) {
	hookLocker.Lock()
	rl, ok := Hooks[name]
	if !ok {
		rl = &RingBuffer{}
		rl.Init(4)
		Hooks[name] = rl
	}
	hookLocker.Unlock()
	for hooks := rl.Clone(); ; hooks.MoveNext() {
		callback(hooks.Read())
	}
}

func AddHookWithContext(ctx context.Context, name string, callback func(interface{})) {
	hookLocker.Lock()
	rl, ok := Hooks[name]
	if !ok {
		rl = NewRingBuffer(4)
		Hooks[name] = rl
	}
	hookLocker.Unlock()
	for hooks := rl.Clone(); ctx.Err() == nil; hooks.MoveNext() {
		callback(hooks.Read())
	}
}

func TriggerHook(name string ,payload interface{}) {
	hookLocker.Lock()
	defer hookLocker.Unlock()
	if rl, ok := Hooks[name]; ok {
		rl.Write(payload)
	} else {
		rl = NewRingBuffer(4)
		Hooks[name] = rl
		rl.Write(payload)
	}
}
