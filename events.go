package engine

import (
	"github.com/asaskevich/EventBus"
)

type TransCodeReq struct {
	ISubscriber
	RequestCodec string
}

const (
	Event_REQUEST_PUBLISH    = "RequestPublish" //当前流丢失发布者，或者订阅者订阅了空流时触发
	Event_SUBSCRIBE          = "Subscribe"
	Event_UNSUBSCRIBE        = "UnSubscibe"
	Event_STREAMCLOSE        = "StreamClose"
	Event_PUBLISH            = "Publish"
	Event_REQUEST_TRANSAUDIO = "RequestTransAudio"
)

var Bus = EventBus.New()
