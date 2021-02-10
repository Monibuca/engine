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
		HasVideo:    true,
		HasAudio:    true,
		EnableAudio: &config.EnableAudio,
		EnableVideo: &config.EnableVideo,
	})
	result = item.(*Stream)
	if !loaded {
		result.Context, result.cancel = context.WithCancel(context.Background())
		if config.EnableVideo {
			result.EnableVideo = &result.HasVideo
		}
		if config.EnableAudio {
			result.EnableAudio = &result.HasAudio
		}
		result.AddVideoTrack()
		result.AddAudioTrack()
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
	VideoTracks    []*VideoTrack
	AudioTracks    []*AudioTrack
	HasAudio       bool
	HasVideo       bool
	EnableVideo    *bool
	EnableAudio    *bool
	subscribeMutex sync.Mutex
}

func (r *Stream) AddVideoTrack() (vt *VideoTrack) {
	vt = new(VideoTrack)
	vt.WaitFirst = make(chan struct{})
	vt.Buffer = NewRing_Video()
	r.VideoTracks = append(r.VideoTracks, vt)
	return
}
func (r *Stream) AddAudioTrack() (at *AudioTrack) {
	at = new(AudioTrack)
	at.Buffer = NewRing_Audio()
	r.AudioTracks = append(r.AudioTracks, at)
	return
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
		utils.Print(Sprintf(Yellow("subscribe :%s %s,to Stream %s"), Blue(r.Type), Cyan(s.ID), BrightCyan(r.StreamPath)))
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
func (r *Stream) PushVideo(ts uint32, payload []byte) {
	r.VideoTracks[0].Push(ts, payload)
}
func (r *Stream) PushAudio(ts uint32, payload []byte) {
	r.AudioTracks[0].Push(ts, payload)
}
