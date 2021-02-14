package engine

import (
	"context"
	"sync"
	"time"

	utils "github.com/Monibuca/utils/v3"
	. "github.com/logrusorgru/aurora"
)

// Streams 所有的流集合
var Streams sync.Map

//FindStream 根据流路径查找流
func FindStream(streamPath string) *Stream {
	if s, ok := Streams.Load(streamPath); ok {
		return s.(*Stream)
	}
	return nil
}

//GetStream 根据流路径获取流，如果不存在则创建一个新的
func GetStream(streamPath string) (result *Stream) {
	item, loaded := Streams.LoadOrStore(streamPath, &Stream{
		StreamPath:  streamPath,
		AudioTracks: make(map[string]*AudioTrack),
		VideoTracks: make(map[string]*VideoTrack),
	})
	result = item.(*Stream)
	if !loaded {
		result.Context, result.cancel = context.WithCancel(context.Background())
		utils.Print(Green("Stream create:"), BrightCyan(streamPath))
	}
	return
}

// Stream 流定义
type Stream struct {
	context.Context
	cancel     context.CancelFunc
	StreamPath string
	StartTime  time.Time //流的创建时间
	*Publisher
	Subscribers    []*Subscriber // 订阅者
	VideoTracks    map[string]*VideoTrack
	AudioTracks    map[string]*AudioTrack
	subscribeMutex sync.Mutex
	audioRW        sync.RWMutex
	videoRW        sync.RWMutex
}

func (r *Stream) AddVideoTrack(codec string, vt *VideoTrack) *VideoTrack {
	if vt == nil {
		vt = NewVideoTrack()
	}
	r.videoRW.Lock()
	r.VideoTracks[codec] = vt
	r.videoRW.Unlock()
	return vt
}

func (r *Stream) AddAudioTrack(codec string, at *AudioTrack) *AudioTrack {
	if at == nil {
		at = NewAudioTrack()
	}
	r.audioRW.Lock()
	r.AudioTracks[codec] = at
	r.audioRW.Unlock()
	return at
}

func (r *Stream) Close() {
	r.cancel()
	utils.Print(Yellow("Stream destoryed :"), BrightCyan(r.StreamPath))
	Streams.Delete(r.StreamPath)
	TriggerHook(Hook{HOOK_STREAMCLOSE, r})
}

//Subscribe 订阅流
func (r *Stream) Subscribe(s *Subscriber) {
	if s.Stream = r; r.Err() == nil {
		s.SubscribeTime = time.Now()
		utils.Print(Sprintf(Yellow("subscribe :%s %s,to Stream %s"), Blue(s.Type), Cyan(s.ID), BrightCyan(r.StreamPath)))
		s.Context, s.cancel = context.WithCancel(r)
		r.subscribeMutex.Lock()
		r.Subscribers = append(r.Subscribers, s)
		r.subscribeMutex.Unlock()
		utils.Print(Sprintf(Yellow("%s subscriber %s added remains:%d"), BrightCyan(r.StreamPath), Cyan(s.ID), Blue(len(r.Subscribers))))
		TriggerHook(Hook{HOOK_SUBSCRIBE, s})
	}
}

//UnSubscribe 取消订阅流
func (r *Stream) UnSubscribe(s *Subscriber) {
	if r.Err() == nil {
		r.subscribeMutex.Lock()
		r.Subscribers = DeleteSliceItem_Subscriber(r.Subscribers, s)
		r.subscribeMutex.Unlock()
		utils.Print(Sprintf(Yellow("%s subscriber %s removed remains:%d"), BrightCyan(r.StreamPath), Cyan(s.ID), Blue(len(r.Subscribers))))
		TriggerHook(Hook{HOOK_UNSUBSCRIBE, s})
		if len(r.Subscribers) == 0 && (r.Publisher == nil || r.Publisher.AutoUnPublish) {
			r.Close()
		}
	}
}
func DeleteSliceItem_Subscriber(slice []*Subscriber, item *Subscriber) []*Subscriber {
	for i, val := range slice {
		if val == item {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}
