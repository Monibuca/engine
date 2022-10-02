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

type AudioFrame AVFrame[AudioSlice]
type VideoFrame AVFrame[NALUSlice]
type AudioDeConf DecoderConfiguration[AudioSlice]
type VideoDeConf DecoderConfiguration[NALUSlice]
type FLVFrame net.Buffers
type AudioRTP RTPFrame
type VideoRTP RTPFrame
type HasAnnexB interface {
	GetAnnexB() (r net.Buffers)
}

func (f FLVFrame) WriteTo(w io.Writer) (int64, error) {
	t := (net.Buffers)(f)
	return t.WriteTo(w)
}

func copyBuffers(b net.Buffers) (r net.Buffers) {
	return append(r, b...)
}

func (v *VideoFrame) GetAnnexB() (r net.Buffers) {
	r = append(r, codec.NALU_Delimiter2)
	for i, nalu := range v.Raw {
		if i > 0 {
			r = append(r, codec.NALU_Delimiter1)
		}
		r = append(r, nalu...)
	}
	return
}

func (v VideoDeConf) GetAnnexB() (r net.Buffers) {
	for _, nalu := range v.Raw {
		r = append(r, codec.NALU_Delimiter2, nalu)
	}
	return
}

type ISubscriber interface {
	IIO
	receive(string, IIO, *config.Subscribe) error
	GetIO() *IO[config.Subscribe]
	GetConfig() *config.Subscribe
	IsPlaying() bool
	PlayRaw()
	PlayBlock(byte)
	PlayFLV()
	Stop()
}
type PlayContext[T interface {
	GetDecConfSeq() int
	ReadRing() *AVRing[R]
	GetDecoderConfiguration() DecoderConfiguration[R]
}, R RawSlice] struct {
	Track   T
	ring    *AVRing[R]
	confSeq int
	Frame   *AVFrame[R]
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
	SkipTS             uint32 //跳过的时间戳
	FirstAbsTS         uint32 //订阅起始时间戳
}

func (tp *TrackPlayer) ReadVideo() (vp *AVFrame[NALUSlice]) {
	vp = tp.Video.ring.Read(tp.Context)
	tp.Video.Frame = vp
	return
}

func (tp *TrackPlayer) ReadAudio() (ap *AVFrame[AudioSlice]) {
	ap = tp.Audio.ring.Read(tp.Context)
	tp.Audio.Frame = ap
	return
}

// Subscriber 订阅者实体定义
type Subscriber struct {
	IO[config.Subscribe]
	TrackPlayer `json:"-"`
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

//PlayBlock 阻塞式读取数据
func (s *Subscriber) PlayBlock(subType byte) {
	spesic := s.Spesic
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
	sendVideoDecConf := func() {
		s.Video.confSeq = s.Video.Track.DecoderConfiguration.Seq
		spesic.OnEvent(VideoDeConf(s.Video.Track.DecoderConfiguration))
	}
	sendAudioDecConf := func() {
		s.Audio.confSeq = s.Audio.Track.DecoderConfiguration.Seq
		s.Spesic.OnEvent(AudioDeConf(s.Audio.Track.DecoderConfiguration))
	}
	var sendVideoFrame func(*AVFrame[NALUSlice])
	var sendAudioFrame func(*AVFrame[AudioSlice])
	switch subType {
	case SUBTYPE_RAW:
		sendVideoFrame = func(frame *AVFrame[NALUSlice]) {
			// println(frame.Sequence, frame.AbsTime, frame.PTS, frame.DTS, frame.IFrame)
			spesic.OnEvent((*VideoFrame)(frame))
		}
		sendAudioFrame = func(frame *AVFrame[AudioSlice]) {
			spesic.OnEvent((*AudioFrame)(frame))
		}
	case SUBTYPE_RTP:
		var videoSeq, audioSeq uint16
		sendVideoFrame = func(frame *AVFrame[NALUSlice]) {
			for _, p := range frame.RTP {
				videoSeq++
				vp := *p
				vp.Header.Timestamp = vp.Header.Timestamp - s.SkipTS*90
				vp.Header.SequenceNumber = videoSeq
				spesic.OnEvent((VideoRTP)(vp))
			}
		}
		sendAudioFrame = func(frame *AVFrame[AudioSlice]) {
			for _, p := range frame.RTP {
				audioSeq++
				vp := *p
				vp.Header.SequenceNumber = audioSeq
				vp.Header.Timestamp = vp.Header.Timestamp - s.SkipTS*90
				spesic.OnEvent((AudioRTP)(vp))
			}
		}
	case SUBTYPE_FLV:
		flvHeadCache := make([]byte, 15, 15) //内存复用
		sendVideoDecConf = func() {
			s.Video.confSeq = s.Video.Track.DecoderConfiguration.Seq
			spesic.OnEvent(FLVFrame(copyBuffers(s.Video.Track.DecoderConfiguration.FLV)))
		}
		sendAudioDecConf = func() {
			s.Audio.confSeq = s.Audio.Track.DecoderConfiguration.Seq
			spesic.OnEvent(FLVFrame(copyBuffers(s.Audio.Track.DecoderConfiguration.FLV)))
		}
		sendFlvFrame := func(t byte, abs uint32, avcc net.Buffers) {
			flvHeadCache[0] = t
			result := append(FLVFrame{flvHeadCache[:11]}, avcc...)
			ts := abs - s.SkipTS
			dataSize := uint32(util.SizeOfBuffers(avcc))
			util.PutBE(flvHeadCache[1:4], dataSize)
			util.PutBE(flvHeadCache[4:7], ts)
			flvHeadCache[7] = byte(ts >> 24)
			spesic.OnEvent(append(result, util.PutBE(flvHeadCache[11:15], dataSize+11)))
		}
		sendVideoFrame = func(frame *AVFrame[NALUSlice]) {
			// println(frame.Sequence, frame.AbsTime, frame.PTS, frame.DTS, frame.IFrame)
			sendFlvFrame(codec.FLV_TAG_TYPE_VIDEO, frame.AbsTime, frame.AVCC)
		}
		sendAudioFrame = func(frame *AVFrame[AudioSlice]) {
			sendFlvFrame(codec.FLV_TAG_TYPE_AUDIO, frame.AbsTime, frame.AVCC)
		}
	}

	defer s.Info("stop")
	for ctx.Err() == nil {
		hasVideo, hasAudio := s.Video.ring != nil && s.Config.SubVideo, s.Audio.ring != nil && s.Config.SubAudio
		if hasVideo {
			for ctx.Err() == nil {
				var vp *AVFrame[NALUSlice]
				switch vstate {
				case SUBSTATE_INIT:
					s.Video.ring.Ring = s.Video.Track.IDRing
					vp = s.ReadVideo()
					startTime = time.Now()
					s.FirstAbsTS = vp.AbsTime
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
						beforeJump = vp.AbsTime
						// 防止过快消费
						if fast := time.Duration(vp.AbsTime-s.FirstAbsTS)*time.Millisecond - time.Since(startTime); fast > 0 && fast < time.Second {
							time.Sleep(fast)
						}
					}
				case SUBSTATE_NORMAL:
					vp = s.ReadVideo()
				}
				if vp.IFrame && s.Video.decConfChanged() {
					// println(s.Video.confSeq, s.Video.Track.SPSInfo.Width, s.Video.Track.SPSInfo.Height)
					sendVideoDecConf()
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
			if vstate < SUBSTATE_NORMAL {
				continue
			}
		}
		// 正常模式下或者纯音频模式下，音频开始播放
		if hasAudio {
			if !audioSent {
				if s.Audio.Track.IsAAC() {
					sendAudioDecConf()
				}
				audioSent = true
			}
			for {
				ap := s.ReadAudio()
				if ctx.Err() != nil {
					return
				}
				if s.Audio.Track.IsAAC() && s.Audio.decConfChanged() {
					sendAudioDecConf()
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
