package engine

import (
	"bufio"
	"context"
	"io"
	"net"
	"strconv"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"m7s.live/engine/v4/codec"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/config"
	"m7s.live/engine/v4/track"
	"m7s.live/engine/v4/util"
)

const (
	SUBTYPE_RAW = iota
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

func (a AudioFrame) WriteRawTo(w io.Writer) (n int64, err error) {
	aulist := a.AUList.ToBuffers()
	return aulist.WriteTo(w)
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
	Stop(reason ...zapcore.Field)
	Subscribe(streamPath string, sub ISubscriber) error
}

type TrackPlayer struct {
	context.Context
	context.CancelFunc
	AudioReader, VideoReader *track.AVRingReader
	Audio                    *track.Audio
	Video                    *track.Video
}

// Subscriber 订阅者实体定义
type Subscriber struct {
	IO
	Config      *config.Subscribe
	readers     []*track.AVRingReader
	TrackPlayer `json:"-" yaml:"-"`
}

func (s *Subscriber) Subscribe(streamPath string, sub ISubscriber) error {
	return s.receive(streamPath, sub)
}

func (s *Subscriber) GetSubscriber() *Subscriber {
	return s
}

func (s *Subscriber) SetIO(i any) {
	s.IO.SetIO(i)
	if s.Writer != nil && s.Config != nil && s.Config.WriteBufferSize > 0 {
		s.Writer = bufio.NewWriterSize(s.Writer, s.Config.WriteBufferSize)
	}
}
func (s *Subscriber) OnEvent(event any) {
	switch v := event.(type) {
	case Track: //默认接受所有track
		s.AddTrack(v)
	default:
		s.IO.OnEvent(event)
	}
}

func (s *Subscriber) CreateTrackReader(t *track.Media) (result *track.AVRingReader) {
	result = track.NewAVRingReader(t)
	s.readers = append(s.readers, result)
	result.Logger = s.With(zap.String("track", t.Name))
	return
}

func (s *Subscriber) AddTrack(t Track) bool {
	switch v := t.(type) {
	case *track.Video:
		if s.VideoReader != nil || !s.Config.SubVideo {
			return false
		}
		s.VideoReader = s.CreateTrackReader(&v.Media)
		s.Video = v
	case *track.Audio:
		if s.AudioReader != nil || !s.Config.SubAudio {
			return false
		}
		s.AudioReader = s.CreateTrackReader(&v.Media)
		s.Audio = v
	default:
		return false
	}
	s.Info("track+1", zap.String("name", t.GetName()))
	return true
}

func (s *Subscriber) IsPlaying() bool {
	return s.TrackPlayer.Context != nil && s.TrackPlayer.Err() == nil
}

func (s *Subscriber) SubPulse() {
	s.Stream.Receive(SubPulse{s.Spesific.(ISubscriber)})
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
	if s.IO.Err() != nil {
		s.Error("play", zap.Error(s.IO.Err()))
		return
	}
	s.Info("playblock", zap.Uint8("subType", subType))
	s.TrackPlayer.Context, s.TrackPlayer.CancelFunc = context.WithCancel(s.IO)
	defer s.TrackPlayer.CancelFunc()
	ctx := s.TrackPlayer.Context
	conf := s.Config
	hasVideo, hasAudio := s.Video != nil && conf.SubVideo, s.Audio != nil && conf.SubAudio
	stopReason := zap.String("reason", "stop")
	defer s.onStop(&stopReason)
	if !hasAudio && !hasVideo {
		stopReason = zap.String("reason", "play neither video nor audio")
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
			// fmt.Println("v", frame.Sequence, s.VideoReader.AbsTime, s.VideoReader.Delay)
			if frame.AUList.ByteLength == 0 {
				return
			}
			spesic.OnEvent(VideoFrame{frame, s.Video, s.VideoReader.AbsTime, s.VideoReader.GetPTS32(), s.VideoReader.GetDTS32()})
		}
		sendAudioFrame = func(frame *AVFrame) {
			if frame.AUList.ByteLength == 0 {
				return
			}
			// fmt.Println("a", s.AudioReader.Delay)
			// fmt.Println("a", frame.Sequence, s.AudioReader.AbsTime)
			spesic.OnEvent(AudioFrame{frame, s.Audio, s.AudioReader.AbsTime, s.AudioReader.GetPTS32(), s.AudioReader.GetDTS32()})
		}
	case SUBTYPE_RTP:
		var videoSeq, audioSeq uint16
		sendVideoFrame = func(frame *AVFrame) {
			// fmt.Println("v", frame.Sequence, frame.AbsTime, s.VideoReader.AbsTime, frame.IFrame)
			delta := uint32(s.VideoReader.SkipTs * 90 / time.Millisecond)
			frame.RTP.Range(func(vp RTPFrame) bool {
				videoSeq++
				copy := *vp.Packet
				vp.Packet = &copy
				vp.Header.Timestamp = vp.Header.Timestamp - delta
				vp.Header.SequenceNumber = videoSeq
				spesic.OnEvent((VideoRTP)(vp))
				return true
			})
		}

		sendAudioFrame = func(frame *AVFrame) {
			// fmt.Println("a", frame.Sequence, frame.Timestamp, s.AudioReader.AbsTime)
			delta := uint32(s.AudioReader.SkipTs / time.Millisecond * time.Duration(s.AudioReader.Track.SampleRate) / 1000)
			frame.RTP.Range(func(ap RTPFrame) bool {
				audioSeq++
				copy := *ap.Packet
				ap.Packet = &copy
				ap.Header.SequenceNumber = audioSeq
				ap.Header.Timestamp = ap.Header.Timestamp - delta
				spesic.OnEvent((AudioRTP)(ap))
				return true
			})
		}
	case SUBTYPE_FLV:
		flvHeadCache := make([]byte, 15) //内存复用
		sendFlvFrame := func(t byte, ts uint32, avcc ...[]byte) {
			// println(t, ts)
			// fmt.Printf("%d %X %X %d\n", t, avcc[0][0], avcc[0][1], ts)
			flvHeadCache[0] = t
			result := append(FLVFrame{flvHeadCache[:11]}, avcc...)
			dataSize := uint32(util.SizeOfBuffers(avcc))
			if dataSize == 0 {
				return
			}
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
			// fmt.Println(frame.Sequence, s.VideoReader.AbsTime, s.VideoReader.Delay, frame.IFrame)
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
			// fmt.Println(frame.Sequence, s.AudioReader.AbsTime, s.AudioReader.Delay)
			sendFlvFrame(codec.FLV_TAG_TYPE_AUDIO, s.AudioReader.AbsTime, frame.AVCC.ToBuffers()...)
		}
	}

	var subMode = conf.SubMode //订阅模式
	if s.Args.Has(conf.SubModeArgName) {
		subMode, _ = strconv.Atoi(s.Args.Get(conf.SubModeArgName))
	}
	var initState = 0
	var videoFrame, audioFrame *AVFrame
	for ctx.Err() == nil {
		if hasVideo {
			for ctx.Err() == nil {
				err := s.VideoReader.ReadFrame(subMode)
				if err == nil {
					err = ctx.Err()
				}
				if err != nil {
					stopReason = zap.Error(err)
					return
				}
				videoFrame = s.VideoReader.Value
				// fmt.Println("video", s.VideoReader.Track.PreFrame().Sequence-frame.Sequence)
				if videoFrame.IFrame && s.VideoReader.DecConfChanged() {
					s.VideoReader.ConfSeq = s.VideoReader.Track.SequenceHeadSeq
					sendVideoDecConf()
				}
				if hasAudio {
					if audioFrame != nil {
						if util.Conditoinal(conf.SyncMode == 0, videoFrame.Timestamp > audioFrame.Timestamp, videoFrame.WriteTime.After(audioFrame.WriteTime)) {
							// fmt.Println("switch audio", audioFrame.CanRead)
							sendAudioFrame(audioFrame)
							audioFrame = nil
							break
						}
					} else if initState++; initState >= 2 {
						break
					}
				}

				if !conf.IFrameOnly || videoFrame.IFrame {
					sendVideoFrame(videoFrame)
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
					}
				}
				err := s.AudioReader.ReadFrame(subMode)
				if err == nil {
					err = ctx.Err()
				}
				if err != nil {
					stopReason = zap.Error(err)
					return
				}
				audioFrame = s.AudioReader.Value
				// fmt.Println("audio", s.AudioReader.Track.PreFrame().Sequence-frame.Sequence)
				if s.AudioReader.DecConfChanged() {
					s.AudioReader.ConfSeq = s.AudioReader.Track.SequenceHeadSeq
					sendAudioDecConf()
				}
				if hasVideo && videoFrame != nil {
					if util.Conditoinal(conf.SyncMode == 0, audioFrame.Timestamp > videoFrame.Timestamp, audioFrame.WriteTime.After(videoFrame.WriteTime)) {
						sendVideoFrame(videoFrame)
						videoFrame = nil
						break
					}
				}
				if audioFrame.Timestamp >= s.AudioReader.SkipTs {
					sendAudioFrame(audioFrame)
				} else {
					// fmt.Println("skip audio", frame.AbsTime, s.AudioReader.SkipTs)
				}
			}
		}
	}
	if videoFrame != nil {
		videoFrame.ReaderLeave()
	}
	if audioFrame != nil {
		audioFrame.ReaderLeave()
	}
	stopReason = zap.Error(ctx.Err())
}

func (s *Subscriber) onStop(reason *zapcore.Field) {
	if !s.Stream.IsClosed() {
		s.Info("play stop", *reason)
		if !s.Config.Internal {
			s.Stream.Receive(s.Spesific)
		}
	}
}
