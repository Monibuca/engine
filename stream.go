package engine

import (
	"context"
	"sync"
	"time"

	utils "github.com/Monibuca/utils/v3"
	. "github.com/logrusorgru/aurora"
)

type StreamCollection struct {
	sync.RWMutex
	m map[string]*Stream
}

func (sc *StreamCollection) GetStream(streamPath string) *Stream {
	sc.RLock()
	defer sc.RUnlock()
	if s, ok := sc.m[streamPath]; ok {
		return s
	}
	return nil
}
func (sc *StreamCollection) Delete(streamPath string) {
	sc.Lock()
	delete(sc.m, streamPath)
	sc.Unlock()
}

func (sc *StreamCollection) ToList() (r []*Stream) {
	sc.RLock()
	defer sc.RUnlock()
	for _, s := range sc.m {
		r = append(r, s)
	}
	return
}

func init() {
	Streams.m = make(map[string]*Stream)
}

// Streams 所有的流集合
var Streams StreamCollection

//FindStream 根据流路径查找流
func FindStream(streamPath string) *Stream {
	return Streams.GetStream(streamPath)
}

// Stream 流定义
type Stream struct {
	context.Context
	StreamPath     string
	Type           string        //流类型，来自发布者
	StartTime      time.Time     //流的创建时间
	Subscribers    []*Subscriber // 订阅者
	VideoTracks    Tracks
	AudioTracks    Tracks
	AutoUnPublish  bool              //	当无人订阅时自动停止发布
	Transcoding    map[string]string //转码配置，key：目标编码，value：发布者提供的编码
	subscribeMutex sync.Mutex
	timeout        *time.Timer //更新时间用来做超时处理
	Close          func()      `json:"-"`
	prePayload     uint32      //需要预拼装ByteStream格式的数据的订阅者数量
}

func (r *Stream) Update() {
	r.timeout.Reset(config.PublishTimeout)
}

// Publish 发布者进行发布操作
func (r *Stream) Publish() bool {
	Streams.Lock()
	defer Streams.Unlock()
	if _, ok := Streams.m[r.StreamPath]; ok {
		return false
	}
	r.VideoTracks.Init()
	r.AudioTracks.Init()
	var cancel context.CancelFunc
	customClose := r.Close
	r.Context, cancel = context.WithCancel(context.Background())
	var closeOnce sync.Once
	r.Close = func() {
		closeOnce.Do(func() {
			r.timeout.Stop()
			if customClose != nil {
				customClose()
			}
			cancel()
			r.VideoTracks.Dispose()
			r.AudioTracks.Dispose()
			utils.Print(Yellow("Stream destoryed :"), BrightCyan(r.StreamPath))
			Streams.Delete(r.StreamPath)
			TriggerHook(Hook{HOOK_STREAMCLOSE, r})
		})
	}
	r.StartTime = time.Now()
	Streams.m[r.StreamPath] = r
	utils.Print(Green("Stream publish:"), BrightCyan(r.StreamPath))
	r.timeout = time.AfterFunc(config.PublishTimeout, r.Close)
	//触发钩子
	TriggerHook(Hook{HOOK_PUBLISH, r})
	return true
}

func (r *Stream) WaitVideoTrack(codecs ...string) *VideoTrack {
	if !config.EnableVideo {
		return nil
	}
	if track := r.VideoTracks.WaitTrack(codecs...); track != nil {
		return track.(*VideoTrack)
	}
	return nil
}

// TODO: 触发转码逻辑
func (r *Stream) WaitAudioTrack(codecs ...string) *AudioTrack {
	if !config.EnableAudio {
		return nil
	}
	if track := r.AudioTracks.WaitTrack(codecs...); track != nil {
		return track.(*AudioTrack)
	}
	return nil
}

//Subscribe 订阅流
func (r *Stream) Subscribe(s *Subscriber) {
	if s.Stream = r; r.Err() == nil {
		s.SubscribeTime = time.Now()
		utils.Print(Sprintf(Yellow("subscribe :%s %s,to Stream %s"), Blue(s.Type), Cyan(s.ID), BrightCyan(r.StreamPath)))
		s.Context, s.cancel = context.WithCancel(r)
		r.subscribeMutex.Lock()
		if s.ByteStreamFormat {
			r.prePayload++
		}
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
		if s.ByteStreamFormat {
			r.prePayload--
		}
		r.Subscribers, deleted = DeleteSliceItem_Subscriber(r.Subscribers, s)
		r.subscribeMutex.Unlock()
		if deleted {
			utils.Print(Sprintf(Yellow("%s subscriber %s removed remains:%d"), BrightCyan(r.StreamPath), Cyan(s.ID), Blue(len(r.Subscribers))))
			TriggerHook(Hook{HOOK_UNSUBSCRIBE, s})
			if len(r.Subscribers) == 0 && r.AutoUnPublish {
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
