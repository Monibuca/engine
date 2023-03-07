package engine

import (
	"context"
	"io"
	"net"
	"strconv"

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
	*track.Audio
	AbsTime uint32
	PTS     uint32
	DTS     uint32
}
type VideoFrame struct {
	*AVFrame
	*track.Video
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
func (f FLVFrame) IsAudio() bool {
	return f[0][0] == codec.FLV_TAG_TYPE_AUDIO
}
func (f FLVFrame) IsVideo() bool {
	return f[0][0] == codec.FLV_TAG_TYPE_VIDEO
}
func (f FLVFrame) WriteTo(w io.Writer) (int64, error) {
	t := (net.Buffers)(f)
	return t.WriteTo(w)
}

func (a AudioFrame) GetADTS() (r net.Buffers) {
	r = append(append(r, a.ADTS.Value), a.AUList.ToBuffers()...)
	return
}

func (v VideoFrame) GetAnnexB() (r net.Buffers) {
	if v.IFrame {
		r = v.ParamaterSets.GetAnnexB()
	}
	v.AUList.Range(func(au *util.BLL) bool {
		r = append(append(r, codec.NALU_Delimiter2), au.ToBuffers()...)
		return true
	})
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

func (s *Subscriber) CreateTrackReader(t *track.Media) (result track.AVRingReader) {
	result.Poll = s.Config.Poll
	result.Track = t
	result.Logger = s.With(zap.String("track", t.Name))
	return
}

func (s *Subscriber) AddTrack(t Track) bool {
	switch v := t.(type) {
	case *track.Video:
		if s.VideoReader.Track != nil || !s.Config.SubVideo {
			return false
		}
		s.VideoReader = s.CreateTrackReader(&v.Media)
		s.Video = v
	case *track.Audio:
		if s.AudioReader.Track != nil || !s.Config.SubAudio {
			return false
		}
		s.AudioReader = s.CreateTrackReader(&v.Media)
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
	s.TrackPlayer.Context, s.TrackPlayer.CancelFunc = context.WithCancel(s.IO)
	ctx := s.TrackPlayer.Context
	conf := s.Config
	hasVideo, hasAudio := s.Video != nil && conf.SubVideo, s.Audio != nil && conf.SubAudio
	defer s.onStop()
	if !hasAudio && !hasVideo {
		s.Error("play neither video nor audio")
		return
	}
	sendVideoDecConf := func() {
		// s.Debug("sendVideoDecConf")
		spesic.OnEvent(s.Video.ParamaterSets)
		spesic.OnEvent(VideoDeConf(s.VideoReader.Track.SequenceHead))
	}
	sendAudioDecConf := func() {
		// s.Debug("sendAudioDecConf")
		spesic.OnEvent(AudioDeConf(s.AudioReader.Track.SequenceHead))
	}
	var sendAudioFrame, sendVideoFrame func(*AVFrame)
	switch subType {
	case SUBTYPE_RAW:
		sendVideoFrame = func(frame *AVFrame) {
			// println("v", frame.Sequence, frame.AbsTime, s.VideoReader.AbsTime, frame.IFrame)
			spesic.OnEvent(VideoFrame{frame, s.Video, s.VideoReader.AbsTime, frame.PTS - s.VideoReader.SkipRTPTs, frame.DTS - s.VideoReader.SkipRTPTs})
		}
		sendAudioFrame = func(frame *AVFrame) {
			// println("a", frame.Sequence, frame.AbsTime, s.AudioReader.AbsTime)
			spesic.OnEvent(AudioFrame{frame, s.Audio, s.AudioReader.AbsTime, frame.PTS - s.AudioReader.SkipRTPTs, frame.PTS - s.AudioReader.SkipRTPTs})
		}
	case SUBTYPE_RTP:
		var videoSeq, audioSeq uint16
		sendVideoFrame = func(frame *AVFrame) {
			// fmt.Println("v", frame.Sequence, frame.AbsTime, s.VideoReader.AbsTime, frame.IFrame)
			frame.RTP.Range(func(vp RTPFrame) bool {
				videoSeq++
				vp.Header.Timestamp = vp.Header.Timestamp - s.VideoReader.SkipRTPTs
				vp.Header.SequenceNumber = videoSeq
				spesic.OnEvent((VideoRTP)(vp))
				return true
			})
		}

		sendAudioFrame = func(frame *AVFrame) {
			// fmt.Println("a", frame.Sequence, frame.AbsTime, s.AudioReader.AbsTime)
			frame.RTP.Range(func(ap RTPFrame) bool {
				audioSeq++
				ap.Header.SequenceNumber = audioSeq
				ap.Header.Timestamp = ap.Header.Timestamp - s.AudioReader.Track.MpegTs2RTPTs(s.AudioReader.SkipRTPTs)
				spesic.OnEvent((AudioRTP)(ap))
				return true
			})
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
			sendFlvFrame(codec.FLV_TAG_TYPE_VIDEO, s.VideoReader.AbsTime, s.VideoReader.Track.SequenceHead)
		}
		sendAudioDecConf = func() {
			sendFlvFrame(codec.FLV_TAG_TYPE_AUDIO, s.AudioReader.AbsTime, s.AudioReader.Track.SequenceHead)
		}
		sendVideoFrame = func(frame *AVFrame) {
			// println(frame.Sequence, s.VideoReader.AbsTime, frame.DeltaTime, frame.IFrame)
			// b := util.Buffer(frame.AVCC.ToBytes()[5:])
			// for b.CanRead() {
			// 	nalulen := int(b.ReadUint32())
			// 	if b.CanReadN(nalulen) {
			// 		bb := b.ReadN(int(nalulen))
			// 		println(nalulen, codec.ParseH264NALUType(bb[0]))
			// 	} else {
			// 		println("error")
			// 	}
			// }
			sendFlvFrame(codec.FLV_TAG_TYPE_VIDEO, s.VideoReader.AbsTime, frame.AVCC.ToBuffers()...)
		}
		sendAudioFrame = func(frame *AVFrame) {
			// println(frame.Sequence, s.AudioReader.AbsTime, frame.DeltaTime)
			sendFlvFrame(codec.FLV_TAG_TYPE_AUDIO, s.AudioReader.AbsTime, frame.AVCC.ToBuffers()...)
		}
	}

	var subMode = conf.SubMode //订阅模式
	if s.Args.Has(conf.SubModeArgName) {
		subMode, _ = strconv.Atoi(s.Args.Get(conf.SubModeArgName))
	}
	var videoFrame, audioFrame *AVFrame
	var lastAbsTime uint32

	for ctx.Err() == nil {
		if hasVideo {
			for ctx.Err() == nil {
				s.VideoReader.Read(ctx, subMode)
				frame := s.VideoReader.Frame
				if frame == nil || ctx.Err() != nil {
					return
				}
				// fmt.Println("video", s.VideoReader.Track.PreFrame().Sequence-frame.Sequence)
				if frame.IFrame && s.VideoReader.DecConfChanged() {
					s.VideoReader.ConfSeq = s.VideoReader.Track.SequenceHeadSeq
					sendVideoDecConf()
				}
				if hasAudio {
					if audioFrame != nil {
						if frame.AbsTime > lastAbsTime {
							// fmt.Println("switch audio", audioFrame.CanRead)
							if audioFrame.CanRead {
								sendAudioFrame(audioFrame)
							}
							videoFrame = frame
							lastAbsTime = frame.AbsTime
							break
						}
					} else if lastAbsTime == 0 {
						if lastAbsTime = frame.AbsTime; lastAbsTime != 0 {
							videoFrame = frame
							break
						}
					}
				}
				if !conf.IFrameOnly || frame.IFrame {
					sendVideoFrame(frame)
				} else {
					// fmt.Println("skip video", frame.Sequence)
				}
			}
		}
		// 正常模式下或者纯音频模式下，音频开始播放
		if hasAudio {
			for ctx.Err() == nil {
				switch s.AudioReader.State {
				case track.READSTATE_INIT:
					if s.Video != nil {
						s.AudioReader.FirstTs = s.VideoReader.FirstTs

					}
				case track.READSTATE_NORMAL:
					if s.Video != nil {
						s.AudioReader.SkipTs = s.VideoReader.SkipTs
						s.AudioReader.SkipRTPTs = s.AudioReader.Track.Ms2MpegTs(s.AudioReader.SkipTs)
					}
				}
				s.AudioReader.Read(ctx, subMode)
				frame := s.AudioReader.Frame
				if frame == nil || ctx.Err() != nil {
					return
				}
				// fmt.Println("audio", s.AudioReader.Track.PreFrame().Sequence-frame.Sequence)
				if s.AudioReader.DecConfChanged() {
					s.AudioReader.ConfSeq = s.AudioReader.Track.SequenceHeadSeq
					sendAudioDecConf()
				}
				if hasVideo && videoFrame != nil {
					if frame.AbsTime > lastAbsTime {
						// fmt.Println("switch video", videoFrame.CanRead)
						if videoFrame.CanRead {
							sendVideoFrame(videoFrame)
						}
						audioFrame = frame
						lastAbsTime = frame.AbsTime
						break
					}
				}
				if frame.AbsTime >= s.AudioReader.SkipTs {
					sendAudioFrame(frame)
				} else {
					// fmt.Println("skip audio", frame.AbsTime, s.AudioReader.SkipTs)
				}
			}
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
func (pub *Pusher) Reconnect() (result bool) {
	result = pub.Config.RePush == -1 || pub.ReConnectCount <= pub.Config.RePush
	pub.ReConnectCount++
	return
}
