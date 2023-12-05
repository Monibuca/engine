package engine

import (
	"reflect"
	"time"

	"m7s.live/engine/v4/common"
)

type Event[T any] struct {
	Time   time.Time
	Target T `json:"-" yaml:"-"`
}

func CreateEvent[T any](target T) (event Event[T]) {
	event.Time = time.Now()
	event.Target = target
	return
}

// PulseEvent 心跳事件
type PulseEvent struct {
	Event[struct{}]
}

type StreamEvent struct {
	Event[*Stream]
}

// StateEvent 状态机事件
type StateEvent struct {
	StreamEvent
	Action StreamAction
	From   StreamState
}

// ErrorEvent 错误事件
type ErrorEvent struct {
	Event[any]
	Error error
}

func (se StateEvent) Next() (next StreamState, ok bool) {
	next, ok = StreamFSM[se.From][se.Action]
	return
}

type SEwaitPublish struct {
	StateEvent
	Publisher IPublisher
}

type SEpublish struct {
	StateEvent
}

type SEtrackAvaliable struct {
	StateEvent
}

type SErepublish struct {
	StateEvent
}

type SEwaitClose struct {
	StateEvent
}
type SEclose struct {
	StateEvent
}
type SEcreate struct {
	StreamEvent
}

type SEKick struct {
	Event[struct{}]
}

type UnsubscribeEvent struct {
	Event[ISubscriber]
}

type AddTrackEvent struct {
	Event[common.Track]
}

// InvitePublishEvent 邀请推流事件(按需拉流)
type InvitePublish struct {
	Event[string]
}

func TryInvitePublish(streamPath string) {
	s := Streams.Get(streamPath)
	if s == nil || s.Publisher == nil {
		EventBus <- InvitePublish{Event: CreateEvent(streamPath)}
	}
}

// InviteTrackEvent 邀请推送指定 Track 事件(转码需要)
type InviteTrackEvent struct {
	Event[string]
	ISubscriber
}

func InviteTrack(name string, suber ISubscriber) {
	EventBus <- InviteTrackEvent{Event: CreateEvent(name), ISubscriber: suber}
}

var handlers = make(map[reflect.Type][]any)

func ListenEvent[T any](handler func(event T)) {
	t := reflect.TypeOf(handler).In(0)
	handlers[t] = append(handlers[t], handler)
}

func EmitEvent[T any](event T) {
	t := reflect.TypeOf(event)
	for _, handler := range handlers[t] {
		handler.(func(event T))(event)
	}
}
