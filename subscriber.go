package engine

import (
	"time"

	. "github.com/Monibuca/engine/v4/common"
	"github.com/Monibuca/engine/v4/config"
	"github.com/Monibuca/engine/v4/track"
)

type AudioFrame AVFrame[AudioSlice]
type VideoFrame AVFrame[NALUSlice]
type ISubscriber interface {
	IIO
	receive(string, ISubscriber, *config.Subscribe) bool
	config.SubscribeConfig
}

// Subscriber 订阅者实体定义
type Subscriber struct {
	IO[config.Subscribe, ISubscriber]
	AudioTrack *track.Audio
	VideoTrack *track.Video
	vr         *AVRing[NALUSlice]
	ar         *AVRing[AudioSlice]
}

func (p *Publisher) Unsubscribe() {
	p.bye(p)
}

func (s *Subscriber) GetSubscribeConfig() *config.Subscribe {
	return s.Config
}

func (s *Subscriber) OnEvent(event any) any {
	s.IO.OnEvent(event)
	switch v := event.(type) {
	case TrackRemoved:
		if a, ok := v.(*track.Audio); ok && a == s.AudioTrack {
			s.ar = nil
		} else if v, ok := v.(*track.Video); ok && v == s.VideoTrack {
			s.vr = nil
		}
	}
	return event
}

func (s *Subscriber) AcceptTrack(t Track) {
	if v, ok := t.(*track.Video); ok {
		s.VideoTrack = v
		s.vr = v.ReadRing()
		go s.play()
	} else if a, ok := t.(*track.Audio); ok {
		s.AudioTrack = a
		s.ar = a.ReadRing()
		if !s.Config.SubVideo {
			go s.play()
		}
	}
	// TODO: data track
}

//Play 开始播放
func (s *Subscriber) play() {
	var t time.Time
	for s.Err() == nil {
		if s.vr != nil {
			for {
				vp := s.vr.Read(s)
				s.OnEvent((*VideoFrame)(vp))
				s.vr.MoveNext()
				if vp.Timestamp.After(t) {
					t = vp.Timestamp
					break
				}
			}
		}
		if s.ar != nil {
			for {
				ap := s.ar.Read(s)
				s.OnEvent((*AudioFrame)(ap))
				s.ar.MoveNext()
				if ap.Timestamp.After(t) {
					t = ap.Timestamp
					break
				}
			}
		}
	}
	return
}

type Pusher struct {
	Subscriber
	Config    *config.Push
	RemoteURL string
	PushCount int
}

// 是否需要重连
func (pub *Pusher) Reconnect() bool {
	return pub.Config.RePush == -1 || pub.PushCount <= pub.Config.RePush
}
