package engine

import (
	"context"
	"time"

	. "github.com/Monibuca/engine/v4/common"
	"github.com/Monibuca/engine/v4/config"
	"github.com/Monibuca/engine/v4/track"
	"go.uber.org/zap"
)

type AudioFrame *AVFrame[AudioSlice]
type VideoFrame *AVFrame[NALUSlice]
type AudioDeConf DecoderConfiguration[AudioSlice]
type VideoDeConf DecoderConfiguration[NALUSlice]
type ISubscriber interface {
	IIO
	receive(string, ISubscriber, *config.Subscribe) bool
	config.SubscribeConfig
	GetSubscriber() *Subscriber
	IsPlaying() bool
	Play(ISubscriber)
	Stop()
}
type TrackPlayer struct {
	context.Context
	context.CancelFunc
	AudioTrack *track.Audio
	VideoTrack *track.Video
	vr         *AVRing[NALUSlice]
	ar         *AVRing[AudioSlice]
}

// Subscriber 订阅者实体定义
type Subscriber struct {
	IO[config.Subscribe, ISubscriber]
	TrackPlayer
}

func (p *Subscriber) GetSubscriber() *Subscriber {
	return p
}

func (s *Subscriber) GetSubscribeConfig() *config.Subscribe {
	return s.Config
}

func (s *Subscriber) OnEvent(event any) {
	switch v := event.(type) {
	case TrackRemoved:
		if a, ok := v.Track.(*track.Audio); ok && a == s.AudioTrack {
			s.ar = nil
		} else if v, ok := v.Track.(*track.Video); ok && v == s.VideoTrack {
			s.vr = nil
		}
	case Track: //默认接受所有track
		s.AddTrack(v)
	default:
		s.IO.OnEvent(event)
	}
}

func (s *Subscriber) AddTrack(t Track) bool {
	if v, ok := t.(*track.Video); ok {
		if s.Config.SubVideo {
			if s.VideoTrack != nil {
				return false
			}
			s.VideoTrack = v
			s.vr = v.ReadRing()
			s.Info("track+1", zap.String("name", v.Name))
			return true
		}
	} else if a, ok := t.(*track.Audio); ok {
		if s.Config.SubAudio {
			if s.AudioTrack != nil {
				return false
			}
			s.AudioTrack = a
			s.ar = a.ReadRing()
			s.Info("track+1", zap.String("name", a.Name))
			return true
		}
	}
	return false
	// TODO: data track
}

func (s *Subscriber) IsPlaying() bool {
	return s.TrackPlayer.Context != nil && s.TrackPlayer.Err() == nil
}

func (s *Subscriber) Stop() {
	if s.IsPlaying() {
		s.TrackPlayer.CancelFunc()
	}
}

//Play 开始播放
func (s *Subscriber) Play(spesic ISubscriber) {
	s.Info("play")
	var t time.Time
	var startTime time.Time    //读到第一个关键帧的时间
	var firstIFrame VideoFrame //起始关键帧
	var audioSent bool         //音频是否发送过
	s.TrackPlayer.Context, s.TrackPlayer.CancelFunc = context.WithCancel(s.IO)
	ctx := s.TrackPlayer.Context
	defer s.Info("stop")
	for ctx.Err() == nil {
		if s.vr != nil {
			if startTime.IsZero() {
				startTime = time.Now()
				firstIFrame = (VideoFrame)(s.vr.Read(ctx))
				s.Debug("firstIFrame", zap.Uint32("seq", firstIFrame.Sequence))
				if ctx.Err() != nil {
					return
				}
				spesic.OnEvent(VideoDeConf(s.VideoTrack.DecoderConfiguration))
			}
			for {
				var vp VideoFrame
				// 如果进入正常模式
				if firstIFrame == nil {
					vp = VideoFrame(s.vr.Read(ctx))
					if ctx.Err() != nil {
						return
					}
					spesic.OnEvent(vp)
					s.vr.MoveNext()
				} else {
					if s.VideoTrack.IDRing.Value.Sequence != firstIFrame.Sequence {
						firstIFrame = nil
						s.vr = s.VideoTrack.ReadRing()
						s.Debug("skip to latest key frame")
						continue
					} else {
						vp = VideoFrame(s.vr.Read(ctx))
						if ctx.Err() != nil {
							return
						}
						spesic.OnEvent(vp)
						if fast := time.Duration(vp.AbsTime-firstIFrame.AbsTime)*time.Millisecond - time.Since(startTime); fast > 0 {
							time.Sleep(fast)
						}
						s.vr.MoveNext()
					}
				}
				if vp.Timestamp.After(t) {
					t = vp.Timestamp
					break
				}
			}
		}
		if s.ar != nil && firstIFrame == nil {
			if !audioSent {
				spesic.OnEvent(AudioDeConf(s.AudioTrack.DecoderConfiguration))
				audioSent = true
			}
			for {
				ap := AudioFrame(s.ar.Read(ctx))
				if ctx.Err() != nil {
					return
				}
				spesic.OnEvent(ap)
				s.ar.MoveNext()
				if ap.Timestamp.After(t) {
					t = ap.Timestamp
					break
				}
			}
		}
	}
}

type PushEvent int
type Pusher struct {
	Client[config.Push]
}

// 是否需要重连
func (pub *Pusher) Reconnect() bool {
	return pub.Config.RePush == -1 || pub.ReConnectCount <= pub.Config.RePush
}
