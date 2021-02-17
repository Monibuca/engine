package engine

import (
	"context"
	"sync"
	"time"

	"github.com/Monibuca/utils/v3/codec"
	"github.com/pkg/errors"
)

// Subscriber 订阅者实体定义
type Subscriber struct {
	context.Context `json:"-"`
	cancel          context.CancelFunc
	Ctx2            context.Context `json:"-"`
	*Stream         `json:"-"`
	ID              string
	TotalDrop       int //总丢帧
	TotalPacket     int
	Type            string
	BufferLength    int
	Delay           uint32
	SubscribeTime   time.Time
	Sign            string
	OffsetTime      uint32
	OnAudio         func(pack AudioPack) `json:"-"`
	OnVideo         func(pack VideoPack) `json:"-"`
}

// IsClosed 检查订阅者是否已经关闭
func (s *Subscriber) IsClosed() bool {
	return s.Context != nil && s.Err() != nil
}

// Close 关闭订阅者
func (s *Subscriber) Close() {
	if s.cancel != nil {
		s.UnSubscribe(s)
		s.cancel()
	}
}
func (s *Subscriber) WaitVideoTrack(codec string) *VideoTrack {
	if !config.EnableVideo {
		return nil
	}
	waiter, ok := s.VideoTracks.LoadOrStore(codec, &TrackWaiter{nil, sync.NewCond(new(sync.Mutex))})
	if !ok {
		waiter.(*TrackWaiter).L.Lock()
		waiter.(*TrackWaiter).Wait()
		waiter.(*TrackWaiter).L.Unlock()
	}
	return waiter.(*TrackWaiter).Track.(*VideoTrack)
}
func (s *Subscriber) WaitAudioTrack(codecs ...string) *AudioTrack {
	if !config.EnableAudio {
		return nil
	}

	for _, codec := range codecs {
		if at, ok := s.AudioTracks.Load(codec); ok && at.(*TrackWaiter).Track != nil {
			return at.(*TrackWaiter).Track.(*AudioTrack)
		}
	}
	if HasTranscoder && s.OriginAudioTrack != nil {
		at := s.AddAudioTrack(codecs[0], NewAudioTrack())
		at.SoundFormat = codec.Codec2SoundFormat[codecs[0]]
		TriggerHook(Hook{HOOK_REQUEST_TRANSAUDIO, &TransCodeReq{s, codecs[0]}})
		return at
	}
	var once sync.Once
	c := make(chan *TrackWaiter)
	for _, codec := range codecs {
		at, _ := s.AudioTracks.LoadOrStore(codec, &TrackWaiter{nil, sync.NewCond(new(sync.Mutex))})
		go func(tw *TrackWaiter) {
			tw.L.Lock()
			tw.Wait()
			tw.L.Unlock()
			once.Do(func() {
				c <- tw
			})
		}(at.(*TrackWaiter))
	}
	return (<-c).Track.(*AudioTrack)
}
func (s *Subscriber) GetVideoTrack(codec string) *VideoTrack {
	if !config.EnableVideo {
		return nil
	}
	if waiter, ok := s.VideoTracks.Load(codec); ok && waiter.(*TrackWaiter).Track != nil {
		return waiter.(*TrackWaiter).Track.(*VideoTrack)
	}
	return nil
}
func (s *Subscriber) GetAudioTrack(codecs ...string) (at *AudioTrack) {
	if !config.EnableAudio {
		return nil
	}
	for _, codec := range codecs {
		if at, ok := s.AudioTracks.Load(codec); ok && at.(*TrackWaiter).Track != nil {
			return at.(*TrackWaiter).Track.(*AudioTrack)
		}
	}
	if HasTranscoder && s.OriginAudioTrack != nil {
		at = s.AddAudioTrack(codecs[0], NewAudioTrack())
		at.SoundFormat = codec.Codec2SoundFormat[codecs[0]]
		TriggerHook(Hook{HOOK_REQUEST_TRANSAUDIO, &TransCodeReq{s, codecs[0]}})
	}
	return
}

//Subscribe 开始订阅 将Subscriber与Stream关联
func (s *Subscriber) Subscribe(streamPath string) error {
	if FindStream(streamPath) == nil {
		return errors.Errorf("Stream not found:%s", streamPath)
	}
	GetStream(streamPath).Subscribe(s)
	if s.Context == nil {
		return errors.Errorf("stream not exist:%s", streamPath)
	}
	return nil
}

//Play 开始播放
func (s *Subscriber) Play(at *AudioTrack, vt *VideoTrack) {
	defer s.Close()
	if vt == nil && at == nil {
		return
	}
	if vt == nil {
		s.PlayAudio(at)
		return
	} else if at == nil {
		s.PlayVideo(vt)
		return
	}
	ctx2 := s.Ctx2
	if ctx2 == nil {
		ctx2 = context.TODO()
	}
	select {
	case <-vt.WaitFirst: //等待获取到第一个关键帧
	case <-s.Context.Done():
		return
	case <-ctx2.Done(): //可能等不到关键帧就退出了
		return
	}
	vr := vt.Buffer.SubRing(vt.FirstScreen) //从关键帧开始读取，首屏秒开
	vr.Current.Wait()                       //等到RingBuffer可读
	ar := at.Buffer.SubRing(at.Buffer.Index)
	ar.Current.Wait()
	dropping := false //是否处于丢帧中
	for ctx2.Err() == nil && s.Context.Err() == nil {
		if ar.Current.Timestamp > vr.Current.Timestamp || ar.Current.Timestamp == 0 {
			if !dropping {
				s.OnVideo(vr.Current.VideoPack)
				if vt.Buffer.Index-vr.Index > 128 {
					dropping = true
				}
			} else if vr.Current.NalType == codec.NALU_IDR_Picture {
				dropping = false
			}
			vr.NextR()
		} else {
			if !dropping {
				s.OnAudio(ar.Current.AudioPack)
				if at.Buffer.Index-ar.Index > 128 {
					dropping = true
				}
			}
			ar.NextR()
		}
	}
}
func (s *Subscriber) PlayAudio(at *AudioTrack) {
	ring := at.Buffer.SubRing(at.Buffer.Index)
	ring.Current.Wait()
	droped := 0
	var action, send func()
	drop := func() {
		if at.Buffer.Index-ring.Index < 10 {
			action = send
		} else {
			droped++
		}
	}
	send = func() {
		s.OnAudio(ring.Current.AudioPack)

		//s.BufferLength = pIndex - ring.Index
		//s.Delay = s.AVRing.Timestamp - packet.Timestamp
		if at.Buffer.Index-ring.Index > 128 {
			action = drop
		}
	}
	ctx2 := s.Ctx2
	if ctx2 == nil {
		ctx2 = context.TODO()
	}
	for action = send; ctx2.Err() == nil && s.Context.Err() == nil; ring.NextR() {
		action()
	}
}

func (s *Subscriber) PlayVideo(vt *VideoTrack) {
	ctx2 := s.Ctx2
	if ctx2 == nil {
		ctx2 = context.TODO()
	}
	select {
	case <-vt.WaitFirst:
	case <-s.Context.Done():
		return
	case <-ctx2.Done(): //可能等不到关键帧就退出了
		return
	}
	ring := vt.Buffer.SubRing(vt.FirstScreen)
	ring.Current.Wait()
	droped := 0
	var action, send func()
	drop := func() {
		if ring.Current.NalType == codec.NALU_IDR_Picture {
			action = send
		} else {
			droped++
		}
	}
	send = func() {
		s.OnVideo(ring.Current.VideoPack)
		pIndex := vt.Buffer.Index
		//s.BufferLength = pIndex - ring.Index
		//s.Delay = s.AVRing.Timestamp - packet.Timestamp
		if pIndex-ring.Index > 128 {
			action = drop
		}
	}
	for action = send; ctx2.Err() == nil && s.Context.Err() == nil; ring.NextR() {
		action()
	}
}
