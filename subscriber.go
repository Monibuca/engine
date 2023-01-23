package engine

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"time"

	"go.uber.org/zap"
	"m7s.live/engine/v4/codec"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/config"
	"m7s.live/engine/v4/track"
	"m7s.live/engine/v4/util"
)

const (
	SUBTYPE_RAW = iota
	SUBTYPE_AVCC
	SUBTYPE_RTP
	SUBTYPE_FLV
)
const (
	SUBSTATE_INIT = iota
	SUBSTATE_FIRST
	SUBSTATE_NORMAL
)

type VideoDeConf []byte
type AudioDeConf []byte
type AudioFrame AVFrame
type VideoFrame AVFrame
type FLVFrame net.Buffers
type AudioRTP RTPFrame
type VideoRTP RTPFrame
type HasAnnexB interface {
	GetAnnexB() (r net.Buffers)
}

func (a AudioDeConf) WithOutRTMP() []byte {
	return a[2:]
}

func (v VideoDeConf) WithOutRTMP() []byte {
	return v[5:]
}

func (f FLVFrame) WriteTo(w io.Writer) (int64, error) {
	t := (net.Buffers)(f)
	return t.WriteTo(w)
}

//	func copyBuffers(b net.Buffers) (r net.Buffers) {
//		return append(r, b...)
//	}
func (v *VideoFrame) GetAnnexB() (r net.Buffers) {
	r = append(r, codec.NALU_Delimiter2)
	for slice := v.AUList.Head; slice != nil; slice = slice.Next {
		r = append(r, slice.ToBuffers()...)
		if slice.Next != nil {
			r = append(r, codec.NALU_Delimiter1)
		}
	}
	return
}

func (v *VideoFrame) WriteAnnexBTo(w io.Writer) (n int, err error) {
	var n1 int
	var n2 int64
	if n1, err = w.Write(codec.NALU_Delimiter2); err != nil {
		return
	}
	n += n1
	for slice := v.AUList.Head; slice != nil; slice = slice.Next {
		if n2, err = slice.WriteTo(w); err != nil {
			return
		}
		n += int(n2)
		if slice.Next != nil {
			if n1, err = w.Write(codec.NALU_Delimiter1); err != nil {
				return
			}
			n += n1
		}
	}
	return
}

type ISubscriber interface {
	IIO
	GetSubscriber() *Subscriber
	IsPlaying() bool
	PlayRaw()
	PlayBlock(byte)
	PlayFLV()
	Stop()
}
type PlayContext[T interface {
	GetDecConfSeq() int
	ReadRing() *AVRing
}] struct {
	Track   T
	ring    *AVRing
	confSeq int
	Frame   *AVFrame
}

func (p *PlayContext[T]) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.Track)
}

func (p *PlayContext[T]) init(t T) {
	p.Track = t
	p.ring = t.ReadRing()
}

func (p *PlayContext[T]) decConfChanged() bool {
	return p.confSeq != p.Track.GetDecConfSeq()
}

type TrackPlayer struct {
	context.Context    `json:"-"`
	context.CancelFunc `json:"-"`
	Audio              PlayContext[*track.Audio]
	Video              PlayContext[*track.Video]
	SkipTS             uint32 //跳过的时间戳
}

func (tp *TrackPlayer) ReadVideo() (vp *AVFrame) {
	vp = tp.Video.ring.Read(tp.Context)
	tp.Video.Frame = vp
	return
}

func (tp *TrackPlayer) ReadAudio() (ap *AVFrame) {
	ap = tp.Audio.ring.Read(tp.Context)
	tp.Audio.Frame = ap
	return
}

// Subscriber 订阅者实体定义
type Subscriber struct {
	IO
	IsInternal  bool //是否内部订阅,不放入订阅列表
	Config      *config.Subscribe
	TrackPlayer `json:"-"`
}

func (s *Subscriber) GetSubscriber() *Subscriber {
	return s
}

func (s *Subscriber) OnEvent(event any) {
	switch v := event.(type) {
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
	s.Info("track+1", zap.String("name", t.GetBase().Name))
	return true
}

func (s *Subscriber) IsPlaying() bool {
	return s.TrackPlayer.Context != nil && s.TrackPlayer.Err() == nil
}

func (s *Subscriber) PlayRaw() {
	s.PlayBlock(SUBTYPE_RAW)
}

func (s *Subscriber) PlayFLV() {
	s.PlayBlock(SUBTYPE_FLV)
}

func (s *Subscriber) PlayRTP() {
	s.PlayBlock(SUBTYPE_RTP)
}

// PlayBlock 阻塞式读取数据
func (s *Subscriber) PlayBlock(subType byte) {
	spesic := s.Spesific
	if spesic == nil {
		s.Error("play before subscribe")
		return
	}
	s.Info("playblock")
	var t time.Time                 //最新的音频或者视频的时间戳，用于音视频同步
	var startTime time.Time         //读到第一个关键帧的时间
	var vstate byte = 0             //video发送状态，0：尚未发送首帧，1：已发送首帧，2：已追上正常进度
	var audioSent bool              //音频是否发送过
	var firstSeq, beforeJump uint32 //第一个关键帧的seq
	s.TrackPlayer.Context, s.TrackPlayer.CancelFunc = context.WithCancel(s.IO)
	ctx := s.TrackPlayer.Context
	sendVideoDecConf := func(frame *AVFrame) {
		s.Video.confSeq = s.Video.Track.SequenceHeadSeq
		spesic.OnEvent(VideoDeConf(s.Video.Track.SequenceHead))
	}
	sendAudioDecConf := func(frame *AVFrame) {
		s.Audio.confSeq = s.Audio.Track.SequenceHeadSeq
		spesic.OnEvent(AudioDeConf(s.Audio.Track.SequenceHead))
	}
	var sendVideoFrame func(*AVFrame)
	var sendAudioFrame func(*AVFrame)
	switch subType {
	case SUBTYPE_RAW:
		sendVideoFrame = func(frame *AVFrame) {
			// println(frame.Sequence, frame.AbsTime, frame.PTS, frame.DTS, frame.IFrame)
			spesic.OnEvent((*VideoFrame)(frame))
		}
		sendAudioFrame = func(frame *AVFrame) {
			spesic.OnEvent((*AudioFrame)(frame))
		}
	case SUBTYPE_RTP:
		var videoSeq, audioSeq uint16
		sendVideoFrame = func(frame *AVFrame) {
			for _, p := range frame.RTP {
				videoSeq++
				vp := *p
				vp.Header.Timestamp = vp.Header.Timestamp - s.SkipTS*90
				vp.Header.SequenceNumber = videoSeq
				spesic.OnEvent((VideoRTP)(vp))
			}
		}
		sendAudioFrame = func(frame *AVFrame) {
			for _, p := range frame.RTP {
				audioSeq++
				vp := *p
				vp.Header.SequenceNumber = audioSeq
				vp.Header.Timestamp = vp.Header.Timestamp - s.SkipTS*90
				spesic.OnEvent((AudioRTP)(vp))
			}
		}
	case SUBTYPE_FLV:
		flvHeadCache := make([]byte, 15) //内存复用
		sendFlvFrame := func(t byte, ts uint32, avcc ...[]byte) {
			flvHeadCache[0] = t
			result := append(FLVFrame{flvHeadCache[:11]}, avcc...)
			dataSize := uint32(util.SizeOfBuffers(avcc))
			util.PutBE(flvHeadCache[1:4], dataSize)
			util.PutBE(flvHeadCache[4:7], ts)
			flvHeadCache[7] = byte(ts >> 24)
			spesic.OnEvent(append(result, util.PutBE(flvHeadCache[11:15], dataSize+11)))
		}
		sendVideoDecConf = func(frame *AVFrame) {
			s.Video.confSeq = s.Video.Track.SequenceHeadSeq
			sendFlvFrame(codec.FLV_TAG_TYPE_VIDEO, 0, s.Video.Track.SequenceHead)
			// spesic.OnEvent(FLVFrame(copyBuffers(s.Video.Track.DecoderConfiguration.FLV)))
		}
		sendAudioDecConf = func(frame *AVFrame) {
			s.Audio.confSeq = s.Audio.Track.SequenceHeadSeq
			sendFlvFrame(codec.FLV_TAG_TYPE_AUDIO, 0, s.Audio.Track.SequenceHead)
			// spesic.OnEvent(FLVFrame(copyBuffers(s.Audio.Track.DecoderConfiguration.FLV)))
		}
		sendVideoFrame = func(frame *AVFrame) {
			// println(frame.Sequence, frame.AbsTime, frame.DeltaTime, frame.IFrame)
			sendFlvFrame(codec.FLV_TAG_TYPE_VIDEO, frame.AbsTime-s.SkipTS, frame.AVCC.ToBuffers()...)
		}
		sendAudioFrame = func(frame *AVFrame) {
			sendFlvFrame(codec.FLV_TAG_TYPE_AUDIO, frame.AbsTime-s.SkipTS, frame.AVCC.ToBuffers()...)
		}
	}
	defer s.onStop()
	for ctx.Err() == nil {
		hasVideo, hasAudio := s.Video.ring != nil && s.Config.SubVideo, s.Audio.ring != nil && s.Config.SubAudio
		if hasVideo {
			for ctx.Err() == nil {
				var vp *AVFrame
				switch vstate {
				case SUBSTATE_INIT:
					s.Video.ring.Ring = s.Video.Track.IDRing
					vp = s.ReadVideo()
					startTime = time.Now()
					s.SkipTS = vp.AbsTime
					firstSeq = vp.Sequence
					s.Info("firstIFrame", zap.Uint32("seq", vp.Sequence))
					if s.Config.LiveMode {
						vstate = SUBSTATE_FIRST
					} else {
						vstate = SUBSTATE_NORMAL
					}
				case SUBSTATE_FIRST:
					if s.Video.Track.IDRing.Value.Sequence != firstSeq {
						s.Video.ring.Ring = s.Video.Track.IDRing // 直接跳到最近的关键帧
						vp = s.ReadVideo()
						s.SkipTS = vp.AbsTime - beforeJump
						s.Debug("skip to latest key frame", zap.Uint32("seq", vp.Sequence), zap.Uint32("skipTS", s.SkipTS))
						vstate = SUBSTATE_NORMAL
					} else {
						vp = s.ReadVideo()
						beforeJump = vp.AbsTime - s.SkipTS
						// 防止过快消费
						if fast := time.Duration(beforeJump)*time.Millisecond - time.Since(startTime); fast > 0 && fast < time.Second {
							time.Sleep(fast)
						}
					}
				case SUBSTATE_NORMAL:
					vp = s.ReadVideo()
				}
				if vp.IFrame && s.Video.decConfChanged() {
					// println(s.Video.confSeq, s.Video.Track.SPSInfo.Width, s.Video.Track.SPSInfo.Height)
					sendVideoDecConf(vp)
				}
				if !s.Config.IFrameOnly || vp.IFrame {
					sendVideoFrame(vp)
				}
				s.Video.ring.MoveNext()
				if vp.Timestamp.After(t) {
					t = vp.Timestamp
					break
				}
			}
			// if vstate < SUBSTATE_NORMAL {
			// 	continue
			// }
		}
		// 正常模式下或者纯音频模式下，音频开始播放
		if hasAudio {
			if !audioSent {
				if s.Audio.Track.IsAAC() {
					sendAudioDecConf(nil)
				}
				audioSent = true
			}
			for {
				ap := s.ReadAudio()
				if ctx.Err() != nil {
					return
				}
				if s.Audio.Track.IsAAC() && s.Audio.decConfChanged() {
					sendAudioDecConf(ap)
				}
				sendAudioFrame(ap)
				s.Audio.ring.MoveNext()
				if ap.Timestamp.After(t) {
					t = ap.Timestamp
					break
				}
			}
		}
		if !hasVideo && !hasAudio {
			time.Sleep(time.Second)
		}
	}
}

func (s *Subscriber) onStop() {
	if !s.Stream.IsClosed() {
		s.Info("stop")
		if !s.IsInternal {
			s.Stream.Receive(s.Spesific)
		}
	}
}

type IPusher interface {
	ISubscriber
	Push() error
	Connect() error
	init(string, string, *config.Push)
	Reconnect() bool
}
type Pusher struct {
	ClientIO[config.Push]
}

// 是否需要重连
func (pub *Pusher) Reconnect() bool {
	return pub.Config.RePush == -1 || pub.ReConnectCount <= pub.Config.RePush
}
