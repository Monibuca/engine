package engine

import (
	"context"
	"encoding/json"
	"net"
	"time"

	"go.uber.org/zap"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/config"
	"m7s.live/engine/v4/track"
)

type HaveFLV interface {
	GetFLV() net.Buffers
}
type HaveAVCC interface {
	GetAVCC() net.Buffers
}

type HaveRTP interface {
	GetRTP() []*RTPFrame
}

type AudioFrame AVFrame[AudioSlice]
type VideoFrame AVFrame[NALUSlice]
type AudioDeConf DecoderConfiguration[AudioSlice]
type VideoDeConf DecoderConfiguration[NALUSlice]

func copyBuffers(b net.Buffers) (r net.Buffers) {
	return append(r, b...)
}

func (a *AudioFrame) GetFLV() net.Buffers {
	return copyBuffers(a.FLV)
}
func (a *AudioFrame) GetAVCC() net.Buffers {
	return copyBuffers(a.AVCC)
}
func (a *AudioFrame) GetRTP() []*RTPFrame {
	return a.RTP
}
func (v *VideoFrame) GetFLV() net.Buffers {
	return copyBuffers(v.FLV)
}
func (v *VideoFrame) GetAVCC() net.Buffers {
	return copyBuffers(v.AVCC)
}
func (v *VideoFrame) GetRTP() []*RTPFrame {
	return v.RTP
}
func (a AudioDeConf) GetFLV() net.Buffers {
	return copyBuffers(a.FLV)
}
func (a VideoDeConf) GetFLV() net.Buffers {
	return copyBuffers(a.FLV)
}
func (a AudioDeConf) GetAVCC() net.Buffers {
	return copyBuffers(a.AVCC)
}
func (a VideoDeConf) GetAVCC() net.Buffers {
	return copyBuffers(a.AVCC)
}

type ISubscriber interface {
	IIO
	receive(string, ISubscriber, *config.Subscribe) error
	getIO() *IO[config.Subscribe, ISubscriber]
	GetConfig() *config.Subscribe
	IsPlaying() bool
	PlayBlock()
	Stop()
}
type PlayContext[T interface {
	GetDecConfSeq() int
	ReadRing() *AVRing[R]
}, R RawSlice] struct {
	Track   T
	ring    *AVRing[R]
	confSeq int
	First   AVFrame[R] `json:"-"`
}

func (p *PlayContext[T, R]) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.Track)
}

func (p *PlayContext[T, R]) init(t T) {
	p.Track = t
	p.ring = t.ReadRing()
}
func (p *PlayContext[T, R]) decConfChanged() bool {
	return p.confSeq != p.Track.GetDecConfSeq()
}

type TrackPlayer struct {
	context.Context    `json:"-"`
	context.CancelFunc `json:"-"`
	Audio              PlayContext[*track.Audio, AudioSlice]
	Video              PlayContext[*track.Video, NALUSlice]
}

// Subscriber 订阅者实体定义
type Subscriber struct {
	IO[config.Subscribe, ISubscriber]
	TrackPlayer `json:"-"`
}

func (s *Subscriber) OnEvent(event any) {
	switch v := event.(type) {
	case TrackRemoved:
		if a, ok := v.Track.(*track.Audio); ok && a == s.Audio.Track {
			s.Audio.ring = nil
		} else if v, ok := v.Track.(*track.Video); ok && v == s.Video.Track {
			s.Video.ring = nil
		}
	case Track: //默认接受所有track
		s.AddTrack(v)
	default:
		s.IO.OnEvent(event)
	}
}

func (s *Subscriber) AddTrack(t Track) bool {
	switch v := t.(type) {
	case *track.Video:
		if s.Video.Track != nil || !s.Config.SubVideo {
			return false
		}
		s.Video.init(v)
	case *track.Audio:
		if s.Audio.Track != nil || !s.Config.SubAudio {
			return false
		}
		s.Audio.init(v)
	case *track.Data:
	default:
		return false
	}
	s.Info("track+1", zap.String("name", t.GetName()))
	return true
}

func (s *Subscriber) IsPlaying() bool {
	return s.TrackPlayer.Context != nil && s.TrackPlayer.Err() == nil
}

// 非阻塞式读取，通过反复调用返回的函数可以尝试读取数据，读取到数据后会调用OnEvent，这种模式自由的在不同的goroutine中调用
// func (s *Subscriber) Play(spesic ISubscriber) func() error {
// 	s.Info("play")
// 	var confSeqa, confSeqv int
// 	var t time.Time
// 	var startTime time.Time     //读到第一个关键帧的时间
// 	var firstIFrame *VideoFrame //起始关键帧
// 	var audioSent bool          //音频是否发送过
// 	s.TrackPlayer.Context, s.TrackPlayer.CancelFunc = context.WithCancel(s.IO)
// 	ctx := s.TrackPlayer.Context
// 	var nextRoundReadAudio bool //下一次读取音频
// 	return func() error {
// 		if ctx.Err() != nil {
// 			return ctx.Err()
// 		}
// 		if !nextRoundReadAudio || s.ar == nil {
// 			if s.Video.ring != nil {
// 				if startTime.IsZero() {
// 					startTime = time.Now()
// 					firstIFrame = (*VideoFrame)(s.Video.ring.Read(ctx)) // 这里阻塞读取为0耗时
// 					s.FirstVideo = *firstIFrame
// 					s.Debug("firstIFrame", zap.Uint32("seq", firstIFrame.Sequence))
// 					if ctx.Err() != nil {
// 						return ctx.Err()
// 					}
// 					confSeqv = s.VideoTrack.DecoderConfiguration.Seq
// 					spesic.OnEvent(VideoDeConf(s.VideoTrack.DecoderConfiguration))
// 					spesic.OnEvent(firstIFrame)
// 					s.Video.ring.MoveNext()
// 					if firstIFrame.Timestamp.After(t) {
// 						t = firstIFrame.Timestamp
// 					}
// 					return nil
// 				} else if firstIFrame == nil {
// 					if vp := (s.Video.ring.TryRead()); vp != nil {
// 						if vp.IFrame && confSeqv != s.VideoTrack.DecoderConfiguration.Seq {
// 							confSeqv = s.VideoTrack.DecoderConfiguration.Seq
// 							spesic.OnEvent(VideoDeConf(s.VideoTrack.DecoderConfiguration))
// 						}
// 						spesic.OnEvent((*VideoFrame)(vp))
// 						s.Video.ring.MoveNext()
// 						// 如果本次读取的视频时间戳比较大，下次给音频一个机会
// 						if nextRoundReadAudio = vp.Timestamp.After(t); nextRoundReadAudio {
// 							t = vp.Timestamp
// 						}
// 						return nil
// 					}
// 				}
// 			} else if s.Config.SubVideo && (s.Stream == nil || s.Stream.Publisher == nil || s.Stream.Publisher.GetConfig().PubVideo) {
// 				// 如果订阅了视频需要等待视频轨道
// 				// TODO: 如果发布配置了视频，订阅配置了视频，但是实际上没有视频，需要处理播放纯音频
// 				return nil
// 			}
// 		}
// 		// 正常模式下或者纯音频模式下，音频开始播放
// 		if s.ar != nil && firstIFrame == nil {
// 			if !audioSent {
// 				confSeqa = s.AudioTrack.DecoderConfiguration.Seq
// 				spesic.OnEvent(AudioDeConf(s.AudioTrack.DecoderConfiguration))
// 				audioSent = true
// 			}
// 			if ap := s.ar.TryRead(); ap != nil {
// 				if s.AudioTrack.DecoderConfiguration.Raw != nil && confSeqa != s.AudioTrack.DecoderConfiguration.Seq {
// 					spesic.OnEvent(AudioDeConf(s.AudioTrack.DecoderConfiguration))
// 				}
// 				spesic.OnEvent(ap)
// 				s.ar.MoveNext()
// 				// 这次如果音频比较大，则下次读取给视频一个机会
// 				if nextRoundReadAudio = !ap.Timestamp.After(t); !nextRoundReadAudio {
// 					t = ap.Timestamp
// 				}
// 				return nil
// 			}
// 		}
// 		return nil
// 	}
// }

//PlayBlock 阻塞式读取数据
func (s *Subscriber) PlayBlock() {
	spesic := s.Spesic
	if spesic == nil {
		s.Error("play before subscribe")
		return
	}
	s.Info("playblock")
	var t time.Time
	var startTime time.Time //读到第一个关键帧的时间
	var normal bool         //正常模式——已追上正常的进度
	var audioSent bool      //音频是否发送过
	s.TrackPlayer.Context, s.TrackPlayer.CancelFunc = context.WithCancel(s.IO)
	ctx := s.TrackPlayer.Context
	defer s.Info("stop")
	for ctx.Err() == nil {
		if s.Video.ring != nil {
			if startTime.IsZero() {
				startTime = time.Now()
				s.Video.First = *(s.Video.ring.Read(ctx)) //不使用指针存储，为了永久保留数据
				s.Debug("firstIFrame", zap.Uint32("seq", s.Video.First.Sequence))
				if ctx.Err() != nil {
					return
				}
				s.sendVideoDecConf()
			}
			for {
				var vp *VideoFrame
				// 如果进入正常模式
				if normal {
					vp = (*VideoFrame)(s.Video.ring.Read(ctx))
					if ctx.Err() != nil {
						return
					}
					if vp.IFrame && s.Video.decConfChanged() {
						s.sendVideoDecConf()
					}
					spesic.OnEvent(vp)
					s.Video.ring.MoveNext()
				} else {
					if s.Video.Track.IDRing.Value.Sequence != s.Video.First.Sequence {
						normal = true
						s.Video.ring = s.Video.Track.ReadRing()
						s.Debug("skip to latest key frame", zap.Uint32("seq", s.Video.Track.IDRing.Value.Sequence))
						continue
					} else {
						vp = (*VideoFrame)(s.Video.ring.Read(ctx))
						if ctx.Err() != nil {
							return
						}
						spesic.OnEvent(vp)
						if fast := time.Duration(vp.AbsTime-s.Video.First.AbsTime)*time.Millisecond - time.Since(startTime); fast > 0 {
							time.Sleep(fast)
						}
						s.Video.ring.MoveNext()
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
		if s.Audio.ring != nil && normal {
			if !audioSent {
				if s.Audio.Track.IsAAC() {
					s.sendAudioDecConf()
				}
				audioSent = true
			}
			for {
				ap := (*AudioFrame)(s.Audio.ring.Read(ctx))
				if ctx.Err() != nil {
					return
				}
				if s.Audio.Track.IsAAC() && s.Audio.decConfChanged() {
					s.sendAudioDecConf()
				}
				spesic.OnEvent(ap)
				s.Audio.ring.MoveNext()
				if ap.Timestamp.After(t) {
					t = ap.Timestamp
					break
				}
			}
		}
	}
}

func (s *Subscriber) sendVideoDecConf() {
	s.Video.confSeq = s.Video.Track.DecoderConfiguration.Seq
	s.Spesic.OnEvent(VideoDeConf(s.Video.Track.DecoderConfiguration))
}
func (s *Subscriber) sendAudioDecConf() {
	s.Audio.confSeq = s.Audio.Track.DecoderConfiguration.Seq
	s.Spesic.OnEvent(AudioDeConf(s.Audio.Track.DecoderConfiguration))
}

type IPusher interface {
	ISubscriber
	Push()
	Connect() error
	init(string, string, *config.Push)
	Reconnect() bool
}
type Pusher struct {
	Client[config.Push]
}

// 是否需要重连
func (pub *Pusher) Reconnect() bool {
	return pub.Config.RePush == -1 || pub.ReConnectCount <= pub.Config.RePush
}
