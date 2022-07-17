package engine

import (
	"io"

	"go.uber.org/zap"
	"m7s.live/engine/v4/codec"
	"m7s.live/engine/v4/codec/mpegts"
	"m7s.live/engine/v4/common"
	"m7s.live/engine/v4/config"
	"m7s.live/engine/v4/track"
)

type IPublisher interface {
	IIO
	GetConfig() *config.Publish
	receive(string, IIO, *config.Publish) error
	GetIO() *IO[config.Publish]
	getAudioTrack() common.AudioTrack
	getVideoTrack() common.VideoTrack
}

type Publisher struct {
	IO[config.Publish]
	common.AudioTrack `json:"-"`
	common.VideoTrack `json:"-"`
}

func (p *Publisher) Stop() {
	p.IO.Stop()
	p.Stream.Receive(ACTION_PUBLISHLOST)
}
func (p *Publisher) getAudioTrack() common.AudioTrack {
	return p.AudioTrack
}
func (p *Publisher) getVideoTrack() common.VideoTrack {
	return p.VideoTrack
}
func (p *Publisher) Equal(p2 IPublisher) bool {
	return p.GetIO() == p2.GetIO()
}
func (p *Publisher) OnEvent(event any) {
	switch v := event.(type) {
	case IPublisher:
		if p.Equal(v) { //第一任

		} else { // 使用前任的track，因为订阅者都挂在前任的上面
			p.AudioTrack = v.getAudioTrack()
			p.VideoTrack = v.getVideoTrack()
		}
	default:
		p.IO.OnEvent(event)
	}
}

func (p *Publisher) WriteAVCCVideo(ts uint32, frame common.AVCCFrame) {
	if p.VideoTrack == nil {
		if frame.IsSequence() {
			ts = 0
			codecID := frame.VideoCodecID()
			switch codecID {
			case codec.CodecID_H264:
				p.VideoTrack = track.NewH264(p.Stream)
			case codec.CodecID_H265:
				p.VideoTrack = track.NewH265(p.Stream)
			default:
				p.Stream.Error("video codecID not support: ", zap.Uint8("codeId", uint8(codecID)))
				return
			}
			p.VideoTrack.WriteAVCC(ts, frame)
		} else {
			p.Stream.Warn("need sequence frame")
		}
	} else {
		p.VideoTrack.WriteAVCC(ts, frame)
	}
}

func (p *Publisher) WriteAVCCAudio(ts uint32, frame common.AVCCFrame) {
	if p.AudioTrack == nil {
		codecID := frame.AudioCodecID()
		switch codecID {
		case codec.CodecID_AAC:
			if !frame.IsSequence() || len(frame) < 4 {
				return
			}
			a := track.NewAAC(p.Stream)
			p.AudioTrack = a
			a.Audio.SampleSize = 16
			a.AVCCHead = []byte{frame[0], 1}
			a.WriteAVCC(0, frame)
		case codec.CodecID_PCMA,
			codec.CodecID_PCMU:
			alaw := true
			if codecID == codec.CodecID_PCMU {
				alaw = false
			}
			a := track.NewG711(p.Stream, alaw)
			p.AudioTrack = a
			a.Audio.SampleRate = uint32(codec.SoundRate[(frame[0]&0x0c)>>2])
			a.Audio.SampleSize = 16
			if frame[0]&0x02 == 0 {
				a.Audio.SampleSize = 8
			}
			a.Channels = frame[0]&0x01 + 1
			a.AVCCHead = frame[:1]
			p.AudioTrack.WriteAVCC(ts, frame)
		default:
			p.Stream.Error("audio codec not support yet", zap.Uint8("codecId", uint8(codecID)))
		}
	} else {
		p.AudioTrack.WriteAVCC(ts, frame)
	}
}

type IPuller interface {
	IPublisher
	Connect() error
	Pull()
	Reconnect() bool
	init(streamPath string, url string, conf *config.Pull)
}

// 用于远程拉流的发布者
type Puller struct {
	ClientIO[config.Pull]
}

// 是否需要重连
func (pub *Puller) Reconnect() (ok bool) {
	ok = pub.Config.RePull == -1 || pub.ReConnectCount <= pub.Config.RePull
	pub.ReConnectCount++
	return
}

type TSPublisher struct {
	Publisher
	*mpegts.MpegTsStream
	adts []byte
}

func (t *TSPublisher) Feed(r io.Reader) error {
	return t.MpegTsStream.Feed(r, t.OnPmtStream, t.OnPES)
}

func (t *TSPublisher) OnEvent(event any) {
	switch v := event.(type) {
	case IPublisher:
		t.MpegTsStream = mpegts.NewMpegTsStream()
		if !t.Equal(v) {
			t.AudioTrack = v.getAudioTrack()
			t.VideoTrack = v.getVideoTrack()
		}
	default:
		t.Publisher.OnEvent(event)
	}
}

func (t *TSPublisher) OnPmtStream(s mpegts.MpegTsPmtStream) {
	switch s.StreamType {
	case mpegts.STREAM_TYPE_H264:
		if t.VideoTrack == nil {
			t.VideoTrack = track.NewH264(t.Publisher.Stream)
		}
	case mpegts.STREAM_TYPE_H265:
		if t.VideoTrack == nil {
			t.VideoTrack = track.NewH265(t.Publisher.Stream)
		}
	case mpegts.STREAM_TYPE_AAC:
		if t.AudioTrack == nil {
			t.AudioTrack = track.NewAAC(t.Publisher.Stream)
		}
	default:
		t.Warn("unsupport stream type:", zap.Uint8("type", s.StreamType))
	}
}

func (t *TSPublisher) OnPES(pes mpegts.MpegTsPESPacket) {
	if pes.Header.Dts == 0 {
		pes.Header.Dts = pes.Header.Pts
	}
	switch pes.Header.StreamID & 0xF0 {
	case mpegts.STREAM_ID_AUDIO:
		if t.AudioTrack != nil {
			if t.adts == nil {
				t.adts = append(t.adts, pes.Payload[:7]...)
				t.AudioTrack.WriteADTS(t.adts)
			}
			current := t.AudioTrack.CurrentFrame()
			current.PTS = uint32(pes.Header.Pts)
			current.DTS = uint32(pes.Header.Dts)
			remainLen := len(pes.Payload)
			current.BytesIn += remainLen
			for remainLen > 0 {
				// AACFrameLength(13)
				// xx xxxxxxxx xxx
				frameLen := (int(pes.Payload[3]&3) << 11) | (int(pes.Payload[4]) << 3) | (int(pes.Payload[5]) >> 5)
				if frameLen > remainLen {
					break
				}

				t.AudioTrack.WriteSlice(pes.Payload[7:frameLen])
				pes.Payload = pes.Payload[frameLen:remainLen]
				remainLen -= frameLen
				t.AudioTrack.Flush()
			}
		}
	case mpegts.STREAM_ID_VIDEO:
		if t.VideoTrack != nil {
			t.VideoTrack.WriteAnnexB(uint32(pes.Header.Pts), uint32(pes.Header.Dts), pes.Payload)
		}
	}
}
