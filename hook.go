package engine

import (
	"context"
	"reflect"
	"runtime"
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

func addHookRing(name string) (r *RingBuffer) {
	r = r.Init(context.TODO(), 10)
	Hooks[name] = r
	return
}

func AddHooks(hooks map[string]interface{}) {
	hookLocker.Lock()
	for name, hook := range hooks {
		rl, ok := Hooks[name]
		if !ok {
			rl = addHookRing(name)
		}
		vf := reflect.ValueOf(hook)
		if vf.Kind() != reflect.Func {
			panic("callback is not a function")
		}
		go rl.Clone().ReadLoop(vf.Call)
	}
	hookLocker.Unlock()
}

func AddHook(name string, callback interface{}) {
	hookLocker.Lock()
	rl, ok := Hooks[name]
	if !ok {
		rl = addHookRing(name)
	}
	hookLocker.Unlock()
	vf := reflect.ValueOf(callback)
	if vf.Kind() != reflect.Func {
		panic("callback is not a function")
	}
	rl.Clone().ReadLoop(vf.Call)
}

func AddHookConditional(name string, callback interface{}, goon func() bool) {
	hookLocker.Lock()
	rl, ok := Hooks[name]
	if !ok {
		rl = addHookRing(name)
	}
	hookLocker.Unlock()
	vf := reflect.ValueOf(callback)
	if vf.Kind() != reflect.Func {
		panic("callback is not a function")
	}
	rl.Clone().ReadLoopConditional(vf.Call, goon)
}

func TriggerHook(name string, payload ...interface{}) {
	args := make([]reflect.Value, len(payload))
	for i, arg := range payload {
		args[i] = reflect.ValueOf(arg)
	}
	defer runtime.Gosched() //防止连续写入
	hookLocker.Lock()
	defer hookLocker.Unlock()
	if rl, ok := Hooks[name]; ok {
		rl.Write(args)
	} else {
		rl = addHookRing(name)
		rl.Write(args)
	}
}
