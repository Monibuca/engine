package engine

import (
	"context"
	"time"

	. "github.com/Monibuca/engine/v4/common"
	"github.com/Monibuca/engine/v4/config"
	"github.com/Monibuca/engine/v4/track"
)

type AudioFrame AVFrame[AudioSlice]
type VideoFrame AVFrame[NALUSlice]
type ISubscriber interface {
	IIO
	receive(string, any, *config.Subscribe) bool
	config.SubscribeConfig
	GetSubscriber() *Subscriber
	Unsubscribe()
}
type TrackPlayer struct {
	context.Context
	context.CancelFunc
	AudioTrack  *track.Audio
	VideoTrack  *track.Video
	vr          *AVRing[NALUSlice]
	ar          *AVRing[AudioSlice]
	startTime   time.Time   //读到第一个关键帧的时间
	firstIFrame *VideoFrame //起始关键帧
}

// Subscriber 订阅者实体定义
type Subscriber struct {
	IO[config.Subscribe]
	TrackPlayer
}

func (p *Subscriber) GetSubscriber() *Subscriber {
	return p
}

func (p *Subscriber) Unsubscribe() {
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

func (s *Subscriber) AddTrack(t Track) bool {
	if v, ok := t.(*track.Video); ok {
		if s.Config.SubVideo {
			if s.VideoTrack != nil {
				return false
			}
			s.VideoTrack = v
			s.vr = v.ReadRing()
			s.firstIFrame = (*VideoFrame)(s.vr.Read(s.TrackPlayer))
			return true
		}
	} else if a, ok := t.(*track.Audio); ok {
		if s.Config.SubAudio {
			if s.AudioTrack != nil {
				return false
			}
			s.AudioTrack = a
			s.ar = a.ReadRing()
			return true
		}
	}
	return false
	// TODO: data track
}

func (s *Subscriber) IsPlaying() bool {
	return s.TrackPlayer.Err() == nil && (s.AudioTrack != nil || s.VideoTrack != nil)
}

//Play 开始播放
func (s *Subscriber) Play() {
	var t time.Time
	for s.TrackPlayer.Err() == nil {
		if s.vr != nil {
			for {
				// 如果进入正常模式
				if s.firstIFrame == nil {
					vp := s.vr.Read(s.TrackPlayer)
					s.OnEvent((*VideoFrame)(vp))
					s.vr.MoveNext()
					if vp.Timestamp.After(t) {
						t = vp.Timestamp
						break
					}
				} else {
					if s.startTime.IsZero() {
						s.startTime = time.Now()
					}
					if &s.VideoTrack.IDRing.Value != (*AVFrame[NALUSlice])(s.firstIFrame) {
						s.firstIFrame = nil
						s.vr = s.VideoTrack.ReadRing()
					} else {
						vp := s.vr.Read(s.TrackPlayer)
						s.OnEvent((*VideoFrame)(vp))
						fast := time.Duration(vp.AbsTime-s.firstIFrame.AbsTime)*time.Millisecond - time.Since(s.startTime)
						if fast > 0 {
							time.Sleep(fast)
						}
						s.vr.MoveNext()
					}
				}
			}
		}
		if s.ar != nil {
			for {
				ap := s.ar.Read(s.TrackPlayer)
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

type PushEvent int
type Pusher struct {
	Client[config.Push]
}

// 是否需要重连
func (pub *Pusher) Reconnect() bool {
	return pub.Config.RePush == -1 || pub.ReConnectCount <= pub.Config.RePush
}
