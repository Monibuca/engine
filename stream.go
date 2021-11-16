package engine

import (
	"context"
	"sync"
	"time"

	utils "github.com/Monibuca/utils/v3"
	. "github.com/logrusorgru/aurora"
	"github.com/pkg/errors"
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

func (sc *StreamCollection) Range(f func(*Stream)) {
	sc.RLock()
	defer sc.RUnlock()
	for _, s := range sc.m {
		f(s)
	}
}

func init() {
	Streams.m = make(map[string]*Stream)
}

// Streams 所有的流集合
var Streams StreamCollection
var StreamTimeoutError = errors.New("timeout")

//FindStream 根据流路径查找流
func FindStream(streamPath string) *Stream {
	return Streams.GetStream(streamPath)
}

// Publish 直接发布
func Publish(streamPath, t string) *Stream {
	var stream = &Stream{
		StreamPath: streamPath,
		Type:       t,
	}
	if stream.Publish() {
		return stream
	}
	return nil
}

type StreamContext struct {
	context.Context
	cancel    context.CancelFunc
	timeout   *time.Timer //更新时间用来做超时处理
	IsTimeout bool
}

func (r *StreamContext) Err() error {
	if r.IsTimeout {
		return StreamTimeoutError
	}
	return r.Context.Err()
}
func (r *StreamContext) Update() {
	if r.timeout != nil {
		r.timeout.Reset(config.PublishTimeout)
	}
}

// Stream 流定义
type Stream struct {
	StreamContext  `json:"-"`
	StreamPath     string
	Type           string        //流类型，来自发布者
	StartTime      time.Time     //流的创建时间
	Subscribers    []*Subscriber // 订阅者
	VideoTracks    Tracks
	AudioTracks    Tracks
	DataTracks     Tracks
	AutoCloseAfter *int               //当无人订阅时延迟N秒后自动停止发布
	Transcoding    map[string]string //转码配置，key：目标编码，value：发布者提供的编码
	subscribeMutex sync.Mutex
	OnClose        func()      `json:"-"`
	ExtraProp      interface{} //额外的属性，用于实现子类化，减少map的使用
	closeDelay     *time.Timer
}

func (r *Stream) Close() {
	Streams.Lock()
	//如果没有发布过，就不需要进行处理
	if r.cancel == nil {
		Streams.Unlock()
		return
	}
	if r.closeDelay != nil {
		r.closeDelay.Stop()
	}
	r.cancel()
	r.cancel = nil
	delete(Streams.m, r.StreamPath)
	Streams.Unlock()
	r.VideoTracks.Dispose()
	r.AudioTracks.Dispose()
	r.DataTracks.Dispose()
	if r.OnClose != nil {
		r.OnClose()
	}
	TriggerHook(HOOK_STREAMCLOSE, r)
	utils.Print(Yellow("Stream destoryed :"), BrightCyan(r.StreamPath))
}

// Publish 发布者进行发布操作
func (r *Stream) Publish() bool {
	Streams.Lock()
	defer Streams.Unlock()
	if _, ok := Streams.m[r.StreamPath]; ok {
		return false
	}
	if r.AutoCloseAfter == nil {
		r.AutoCloseAfter = &config.AutoCloseAfter
	}
	r.Context, r.cancel = context.WithCancel(Ctx)
	r.VideoTracks.Init(r)
	r.AudioTracks.Init(r)
	r.DataTracks.Init(r)
	r.StartTime = time.Now()
	Streams.m[r.StreamPath] = r
	utils.Print(Green("Stream publish:"), BrightCyan(r.StreamPath))
	//触发钩子
	TriggerHook(HOOK_PUBLISH, r)
	go r.waitClose()
	return true
}

// 等待流关闭
func (r *Stream) waitClose() {
	r.timeout = time.NewTimer(config.PublishTimeout)
	defer r.timeout.Stop()
	var closeChann <-chan time.Time
	if *r.AutoCloseAfter > 0 {
		r.closeDelay = time.NewTimer(time.Duration(*r.AutoCloseAfter) * time.Second)
		r.closeDelay.Stop()
		closeChann = r.closeDelay.C
		defer r.closeDelay.Stop()
	}
	select {
	case <-r.Done():
	case <-closeChann:
		utils.Print(Yellow("Stream closeDelay:"), BrightCyan(r.StreamPath))
		r.Close()
	case <-r.timeout.C:
		utils.Print(Yellow("Stream timeout:"), BrightCyan(r.StreamPath))
		r.IsTimeout = true
		r.Close()
	}
}

func (r *Stream) WaitDataTrack(names ...string) *DataTrack {
	if !config.EnableVideo {
		return nil
	}
	if track := r.DataTracks.WaitTrack(names...); track != nil {
		return track.(*DataTrack)
	}
	return nil
}

func (r *Stream) WaitVideoTrack(names ...string) *VideoTrack {
	if !config.EnableVideo {
		return nil
	}
	if track := r.VideoTracks.WaitTrack(names...); track != nil {
		return track.(*VideoTrack)
	}
	return nil
}

// TODO: 触发转码逻辑
func (r *Stream) WaitAudioTrack(names ...string) *AudioTrack {
	if !config.EnableAudio {
		return nil
	}
	if track := r.AudioTracks.WaitTrack(names...); track != nil {
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
		if *r.AutoCloseAfter > 0 {
			r.closeDelay.Stop()
		}
		r.Subscribers = append(r.Subscribers, s)
		TriggerHook(HOOK_SUBSCRIBE, s, len(r.Subscribers))
		r.subscribeMutex.Unlock()
		utils.Print(Sprintf(Yellow("%s subscriber %s added remains:%d"), BrightCyan(r.StreamPath), Cyan(s.ID), Blue(len(r.Subscribers))))
	}
}

//UnSubscribe 取消订阅流
func (r *Stream) UnSubscribe(s *Subscriber) {
	if r.Err() == nil {
		var deleted bool
		r.subscribeMutex.Lock()
		defer r.subscribeMutex.Unlock()
		r.Subscribers, deleted = DeleteSliceItem_Subscriber(r.Subscribers, s)
		if deleted {
			utils.Print(Sprintf(Yellow("%s subscriber %s removed remains:%d"), BrightCyan(r.StreamPath), Cyan(s.ID), Blue(len(r.Subscribers))))
			l := len(r.Subscribers)
			TriggerHook(HOOK_UNSUBSCRIBE, s, l)
			if l == 0 && *r.AutoCloseAfter >= 0 {
				if *r.AutoCloseAfter == 0 {
					r.Close()
				} else {
					r.closeDelay.Reset(time.Duration(*r.AutoCloseAfter) * time.Second)
				}
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
