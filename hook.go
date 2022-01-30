package engine

import (
	"github.com/asaskevich/EventBus"
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

var bus = EventBus.New()

// AddHook add a new hook func and wait for the trigger
func AddHook(name string, callback interface{}) {
	bus.Subscribe(name, callback)

}

func AddHookGo(name string, callback interface{}) {
	bus.Subscribe(name, callback)
}

func TriggerHook(name string, payload ...interface{}) {
	bus.Publish(name, payload...)
}
