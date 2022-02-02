package engine

import (
	"github.com/asaskevich/EventBus"
)

type TransCodeReq struct {
	*Subscriber
	RequestCodec string
}

const (
	Event_SUBSCRIBE          = "Subscribe"
	Event_UNSUBSCRIBE        = "UnSubscibe"
	Event_STREAMCLOSE        = "StreamClose"
	Event_PUBLISH            = "Publish"
	Event_REQUEST_TRANSAUDIO = "RequestTransAudio"
)

var Bus = EventBus.New()