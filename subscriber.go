package engine

import (
	"context"
	"io"
	"net"
	"strconv"
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

// AVCC 格式的序列帧
type VideoDeConf []byte

// AVCC 格式的序列帧
type AudioDeConf []byte
type AudioFrame struct {
	*AVFrame
	AbsTime uint32
	PTS     uint32
	DTS     uint32
}
type VideoFrame struct {
	*AVFrame
	AbsTime uint32
	PTS     uint32
	DTS     uint32
}
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
func (v VideoFrame) GetAnnexB() (r net.Buffers) {
	v.AUList.Range(func(au *util.BLL) bool {
		r = append(append(r, codec.NALU_Delimiter1), au.ToBuffers()...)
		return true
	})
	r[0] = codec.NALU_Delimiter2
	return
}

func (v VideoFrame) WriteAnnexBTo(w io.Writer) (n int64, err error) {
	annexB := v.GetAnnexB()
	return annexB.WriteTo(w)
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

type TrackPlayer struct {
	context.Context
	context.CancelFunc
	AudioReader, VideoReader track.AVRingReader
	Audio                    *track.Audio
	Video                    *track.Video
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
		if s.VideoReader.Track != nil || !s.Config.SubVideo {
			return false
		}
		s.VideoReader.Poll = s.Config.Poll
		s.VideoReader.Track = &v.Media
		s.Video = v
	case *track.Audio:
		if s.AudioReader.Track != nil || !s.Config.SubAudio {
			return false
		}
		s.AudioReader.Poll = s.Config.Poll
		s.AudioReader.Track = &v.Media
		s.Audio = v
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
	var lastPlayAbsTime uint32 //最新的音频或者视频的时间戳，用于音视频同步
	s.TrackPlayer.Context, s.TrackPlayer.CancelFunc = context.WithCancel(s.IO)
	ctx := s.TrackPlayer.Context
	sendVideoDecConf := func() {
		spesic.OnEvent(s.Video.ParamaterSets)
		spesic.OnEvent(VideoDeConf(s.VideoReader.Track.SequenceHead))
	}
	sendAudioDecConf := func() {
		spesic.OnEvent(AudioDeConf(s.AudioReader.Track.SequenceHead))
	}
	var sendAudioFrame, sendVideoFrame func(*AVFrame)
	switch subType {
	case SUBTYPE_RAW:
		sendVideoFrame = func(frame *AVFrame) {
			// println("v", frame.Sequence, s.VideoReader.AbsTime, frame.IFrame)
			spesic.OnEvent(VideoFrame{frame, s.VideoReader.AbsTime, frame.PTS - s.VideoReader.SkipTs*90, frame.DTS - s.VideoReader.SkipTs*90})
		}
		sendAudioFrame = func(frame *AVFrame) {
			// println("a", frame.Sequence, s.AudioReader.AbsTime)
			spesic.OnEvent(AudioFrame{frame, s.AudioReader.AbsTime, s.AudioReader.AbsTime * 90, s.AudioReader.AbsTime * 90})
		}
	case SUBTYPE_RTP:
		var videoSeq, audioSeq uint16
		sendVideoFrame = func(frame *AVFrame) {
			for _, p := range frame.RTP {
				videoSeq++
				vp := *p
				vp.Header.Timestamp = vp.Header.Timestamp - s.VideoReader.SkipTs*90
				vp.Header.SequenceNumber = videoSeq
				spesic.OnEvent((VideoRTP)(vp))
			}
		}
		sendAudioFrame = func(frame *AVFrame) {
			for _, p := range frame.RTP {
				audioSeq++
				vp := *p
				vp.Header.SequenceNumber = audioSeq
				vp.Header.Timestamp = vp.Header.Timestamp - s.AudioReader.SkipTs*90
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
		sendVideoDecConf = func() {
			sendFlvFrame(codec.FLV_TAG_TYPE_VIDEO, 0, s.VideoReader.Track.SequenceHead)
			// spesic.OnEvent(FLVFrame(copyBuffers(s.Video.Track.DecoderConfiguration.FLV)))
		}
		sendAudioDecConf = func() {
			sendFlvFrame(codec.FLV_TAG_TYPE_AUDIO, 0, s.AudioReader.Track.SequenceHead)
			// spesic.OnEvent(FLVFrame(copyBuffers(s.Audio.Track.DecoderConfiguration.FLV)))
		}
		sendVideoFrame = func(frame *AVFrame) {
			// println(frame.Sequence, s.VideoReader.AbsTime, frame.DeltaTime, frame.IFrame)
			sendFlvFrame(codec.FLV_TAG_TYPE_VIDEO, s.VideoReader.AbsTime, frame.AVCC.ToBuffers()...)
		}
		sendAudioFrame = func(frame *AVFrame) {
			// println(frame.Sequence, s.AudioReader.AbsTime, frame.DeltaTime)
			sendFlvFrame(codec.FLV_TAG_TYPE_AUDIO, s.AudioReader.AbsTime, frame.AVCC.ToBuffers()...)
		}
	}
	defer s.onStop()
	var subMode = s.Config.SubMode //订阅模式
	if s.Args.Has(s.Config.SubModeArgName) {
		subMode, _ = strconv.Atoi(s.Args.Get(s.Config.SubModeArgName))
	}
	var videoFrame, audioFrame *AVFrame
	for ctx.Err() == nil {
		hasVideo, hasAudio := s.VideoReader.Track != nil && s.Config.SubVideo, s.AudioReader.Track != nil && s.Config.SubAudio
		if hasVideo {
			if videoFrame != nil {
				sendVideoFrame(videoFrame)
				videoFrame = nil
			}
			for ctx.Err() == nil {
				s.VideoReader.Read(ctx, subMode)
				frame := s.VideoReader.Frame
				// println("video", frame.Sequence, frame.AbsTime)
				if frame == nil || ctx.Err() != nil {
					return
				}
				if audioFrame != nil {
					if frame.AbsTime > audioFrame.AbsTime {
						sendAudioFrame(audioFrame)
						audioFrame = nil
					}
				}
				if frame.IFrame && s.VideoReader.DecConfChanged() {
					s.VideoReader.ConfSeq = s.VideoReader.Track.SequenceHeadSeq
					// println(s.Video.confSeq, s.Video.Track.SPSInfo.Width, s.Video.Track.SPSInfo.Height)
					sendVideoDecConf()
				}
				if !s.Config.IFrameOnly || frame.IFrame {
					if frame.AbsTime > lastPlayAbsTime {
						lastPlayAbsTime = frame.AbsTime
						videoFrame = frame
						break
					} else {
						sendVideoFrame(frame)
					}
				}
			}
		}
		// 正常模式下或者纯音频模式下，音频开始播放
		if hasAudio {
			if audioFrame != nil {
				sendAudioFrame(audioFrame)
				audioFrame = nil
			}
			for ctx.Err() == nil {
				switch s.AudioReader.State {
				case track.READSTATE_INIT:
					if s.Video != nil {
						s.AudioReader.FirstTs = s.VideoReader.FirstTs
					}
				case track.READSTATE_NORMAL:
					if s.Video != nil {
						s.AudioReader.SkipTs = s.VideoReader.SkipTs
					}
				}
				s.AudioReader.Read(ctx, subMode)
				frame := s.AudioReader.Frame
				// println("audio", frame.Sequence, frame.AbsTime)
				if frame == nil || ctx.Err() != nil {
					return
				}
				if videoFrame != nil {
					if frame.AbsTime > videoFrame.AbsTime {
						sendVideoFrame(videoFrame)
						videoFrame = nil
					}
				}
				if s.AudioReader.DecConfChanged() {
					s.AudioReader.ConfSeq = s.AudioReader.Track.SequenceHeadSeq
					sendAudioDecConf()
				}
				if frame.AbsTime > lastPlayAbsTime {
					lastPlayAbsTime = frame.AbsTime
					audioFrame = frame
					break
				} else {
					sendAudioFrame(frame)
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
