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
		StreamPath: streamPath,
	})
	result = item.(*Stream)
	if !loaded {
		result.Context, result.cancel = context.WithCancel(context.Background())
		utils.Print(Green("Stream create:"), BrightCyan(streamPath))
	}
	return
}

type TrackWaiter struct {
	Track
	*sync.Cond
}

func (tw *TrackWaiter) Ok(t Track) {
	tw.Track = t
	tw.Broadcast()
}

// Stream 流定义
type Stream struct {
	context.Context
	cancel     context.CancelFunc
	StreamPath string
	StartTime  time.Time //流的创建时间
	*Publisher
	Subscribers      []*Subscriber // 订阅者
	VideoTracks      sync.Map
	AudioTracks      sync.Map
	OriginVideoTrack *VideoTrack //原始视频轨
	OriginAudioTrack *AudioTrack //原始音频轨
	subscribeMutex   sync.Mutex
}

func (r *Stream) SetOriginVT(vt *VideoTrack) {
	r.OriginVideoTrack = vt
	switch vt.CodecID {
	case 7:
		r.AddVideoTrack("h264", vt)
	case 12:
		r.AddVideoTrack("h265", vt)
	}
}
func (r *Stream) SetOriginAT(at *AudioTrack) {
	r.OriginAudioTrack = at
	switch at.SoundFormat {
	case 10:
		r.AddAudioTrack("aac", at)
	case 7:
		r.AddAudioTrack("pcma", at)
	case 8:
		r.AddAudioTrack("pcmu", at)
	}
}
func (r *Stream) AddVideoTrack(codec string, vt *VideoTrack) *VideoTrack {
	vt.Stream = r
	if actual, loaded := r.VideoTracks.LoadOrStore(codec, &TrackWaiter{vt, sync.NewCond(new(sync.Mutex))}); loaded {
		actual.(*TrackWaiter).Ok(vt)
	}
	return vt
}

func (r *Stream) AddAudioTrack(codec string, at *AudioTrack) *AudioTrack {
	at.Stream = r
	if actual, loaded := r.AudioTracks.LoadOrStore(codec, &TrackWaiter{at, sync.NewCond(new(sync.Mutex))}); loaded {
		actual.(*TrackWaiter).Ok(at)
	}
	return at
}

func (r *Stream) Close() {
	r.cancel()
	// if r.OriginVideoTrack != nil {
	// 	r.OriginVideoTrack.Buffer.Current.Done()
	// }
	// if r.OriginAudioTrack != nil {
	// 	r.OriginAudioTrack.Buffer.Current.Done()
	// }
	r.VideoTracks.Range(func(k, v interface{}) bool {
		v.(*TrackWaiter).Broadcast()
		if v.(*TrackWaiter).Track != nil {
			v.(*TrackWaiter).Track.Dispose()
		}
		return true
	})
	r.AudioTracks.Range(func(k, v interface{}) bool {
		v.(*TrackWaiter).Broadcast()
		if v.(*TrackWaiter).Track != nil {
			v.(*TrackWaiter).Track.Dispose()
		}
		return true
	})
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
		var deleted bool
		r.subscribeMutex.Lock()
		r.Subscribers, deleted = DeleteSliceItem_Subscriber(r.Subscribers, s)
		r.subscribeMutex.Unlock()
		if deleted {
			utils.Print(Sprintf(Yellow("%s subscriber %s removed remains:%d"), BrightCyan(r.StreamPath), Cyan(s.ID), Blue(len(r.Subscribers))))
			TriggerHook(Hook{HOOK_UNSUBSCRIBE, s})
			if len(r.Subscribers) == 0 && (r.Publisher == nil || r.Publisher.AutoUnPublish) {
				r.Close()
			}
		}
	}
}
func DeleteSliceItem_Subscriber(slice []*Subscriber, item *Subscriber) ([]*Subscriber, bool) {
	for i, val := range slice {
		if val == item {
			return append(slice[:i], slice[i+1:]...), true
		}
	}
	return slice, false
}
