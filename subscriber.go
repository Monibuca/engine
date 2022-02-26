package engine

import (
	"context"
	"net"
	"time"

	. "github.com/Monibuca/engine/v4/common"
	"github.com/Monibuca/engine/v4/config"
	"github.com/Monibuca/engine/v4/track"
	"go.uber.org/zap"
)

type HaveFLV interface {
	GetFLV() net.Buffers
}
type HaveAVCC interface {
	GetAVCC() net.Buffers
}

type AudioFrame AVFrame[AudioSlice]
type VideoFrame AVFrame[NALUSlice]
type AudioDeConf DecoderConfiguration[AudioSlice]
type VideoDeConf DecoderConfiguration[NALUSlice]

func (a *AudioFrame) GetFLV() net.Buffers {
	return a.FLV
}
func (a *AudioFrame) GetAVCC() net.Buffers {
	return a.AVCC
}
func (a *AudioFrame) GetRTP() []*RTPFrame {
	return a.RTP
}
func (v *VideoFrame) GetFLV() net.Buffers {
	return v.FLV
}
func (v *VideoFrame) GetAVCC() net.Buffers {
	return v.AVCC
}
func (v *VideoFrame) GetRTP() []*RTPFrame {
	return v.RTP
}
func (a *AudioDeConf) GetFLV() net.Buffers {
	return a.FLV
}
func (a *VideoDeConf) GetFLV() net.Buffers {
	return a.FLV
}
func (a *AudioDeConf) GetAVCC() net.Buffers {
	return a.AVCC
}
func (a *VideoDeConf) GetAVCC() net.Buffers {
	return a.AVCC
}

type ISubscriber interface {
	IIO
	receive(string, ISubscriber, *config.Subscribe) error
	GetConfig() *config.Subscribe
	IsPlaying() bool
	Play(ISubscriber) func() error
	PlayBlock(ISubscriber)
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

// 非阻塞式读取，通过反复调用返回的函数可以尝试读取数据，读取到数据后会调用OnEvent，这种模式自由的在不同的goroutine中调用
func (s *Subscriber) Play(spesic ISubscriber) func() error {
	s.Info("play")
	var t time.Time
	var startTime time.Time     //读到第一个关键帧的时间
	var firstIFrame *VideoFrame //起始关键帧
	var audioSent bool          //音频是否发送过
	s.TrackPlayer.Context, s.TrackPlayer.CancelFunc = context.WithCancel(s.IO)
	ctx := s.TrackPlayer.Context
	var nextRoundReadAudio bool //下一次读取音频
	return func() error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !nextRoundReadAudio || s.ar == nil {
			if s.vr != nil {
				if startTime.IsZero() {
					startTime = time.Now()
					firstIFrame = (*VideoFrame)(s.vr.Read(ctx)) // 这里阻塞读取为0耗时
					s.Debug("firstIFrame", zap.Uint32("seq", firstIFrame.Sequence))
					if ctx.Err() != nil {
						return ctx.Err()
					}
					spesic.OnEvent(VideoDeConf(s.VideoTrack.DecoderConfiguration))
					spesic.OnEvent(firstIFrame)
					s.vr.MoveNext()
					if firstIFrame.Timestamp.After(t) {
						t = firstIFrame.Timestamp
					}
					return nil
				} else if firstIFrame == nil {
					if vp := (s.vr.TryRead()); vp != nil {
						spesic.OnEvent((*VideoFrame)(vp))
						s.vr.MoveNext()
						// 如果本次读取的视频时间戳比较大，下次给音频一个机会
						if nextRoundReadAudio = vp.Timestamp.After(t); nextRoundReadAudio {
							t = vp.Timestamp
						}
						return nil
					}
				}
			} else if s.Config.SubVideo && (s.Stream == nil || s.Stream.Publisher == nil || s.Stream.Publisher.GetConfig().PubVideo) {
				// 如果订阅了视频需要等待视频轨道
				// TODO: 如果发布配置了视频，订阅配置了视频，但是实际上没有视频，需要处理播放纯音频
				return nil
			}
		}
		// 正常模式下或者纯音频模式下，音频开始播放
		if s.ar != nil && firstIFrame == nil {
			if !audioSent {
				spesic.OnEvent(AudioDeConf(s.AudioTrack.DecoderConfiguration))
				audioSent = true
			}
			if ap := s.ar.TryRead(); ap != nil {
				spesic.OnEvent(ap)
				s.ar.MoveNext()
				// 这次如果音频比较大，则下次读取给视频一个机会
				if nextRoundReadAudio = !ap.Timestamp.After(t); !nextRoundReadAudio {
					t = ap.Timestamp
				}
				return nil
			}
		}
		return nil
	}
}

//PlayBlock 阻塞式读取数据
func (s *Subscriber) PlayBlock(spesic ISubscriber) {
	s.Info("playblock")
	var t time.Time
	var startTime time.Time     //读到第一个关键帧的时间
	var firstIFrame *VideoFrame //起始关键帧
	var audioSent bool          //音频是否发送过
	s.TrackPlayer.Context, s.TrackPlayer.CancelFunc = context.WithCancel(s.IO)
	ctx := s.TrackPlayer.Context
	defer s.Info("stop")
	for ctx.Err() == nil {
		if s.vr != nil {
			if startTime.IsZero() {
				startTime = time.Now()
				firstIFrame = (*VideoFrame)(s.vr.Read(ctx))
				s.Debug("firstIFrame", zap.Uint32("seq", firstIFrame.Sequence))
				if ctx.Err() != nil {
					return
				}
				spesic.OnEvent(VideoDeConf(s.VideoTrack.DecoderConfiguration))
			}
			for {
				var vp *VideoFrame
				// 如果进入正常模式
				if firstIFrame == nil {
					vp = (*VideoFrame)(s.vr.Read(ctx))
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
						vp = (*VideoFrame)(s.vr.Read(ctx))
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
		} else if s.Config.SubVideo && (s.Stream == nil || s.Stream.Publisher == nil || s.Stream.Publisher.GetConfig().PubVideo) {
			// 如果订阅了视频需要等待视频轨道
			time.Sleep(time.Second)
			continue
		}
		// 正常模式下或者纯音频模式下，音频开始播放
		if s.ar != nil && firstIFrame == nil {
			if !audioSent {
				if s.AudioTrack.IsAAC() {
					spesic.OnEvent(AudioDeConf(s.AudioTrack.DecoderConfiguration))
				}
				audioSent = true
			}
			for {
				ap := (*AudioFrame)(s.ar.Read(ctx))
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
