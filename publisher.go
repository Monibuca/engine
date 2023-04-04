package engine

import (
	"go.uber.org/zap"
	"m7s.live/engine/v4/codec"
	"m7s.live/engine/v4/common"
	"m7s.live/engine/v4/config"
	"m7s.live/engine/v4/track"
	"m7s.live/engine/v4/util"
)

type IPublisher interface {
	IIO
	GetPublisher() *Publisher
	getAudioTrack() common.AudioTrack
	getVideoTrack() common.VideoTrack
}

var _ IPublisher = (*Publisher)(nil)

type Publisher struct {
	IO
	Config            *config.Publish
	common.AudioTrack `json:"-"`
	common.VideoTrack `json:"-"`
}

func (p *Publisher) GetPublisher() *Publisher {
	return p
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
	return p == p2.GetPublisher()
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

func (p *Publisher) WriteAVCCVideo(ts uint32, frame *util.BLL, pool util.BytesPool) {
	if frame.ByteLength < 6 {
		return
	}
	if p.VideoTrack == nil {
		if frame.GetByte(1) == 0 {
			ts = 0
			switch codecID := codec.VideoCodecID(frame.GetByte(0) & 0x0F); codecID {
			case codec.CodecID_H264:
				p.VideoTrack = track.NewH264(p.Stream, pool)
			case codec.CodecID_H265:
				p.VideoTrack = track.NewH265(p.Stream, pool)
			default:
				p.Stream.Error("video codecID not support", zap.Uint8("codeId", uint8(codecID)))
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

func (p *Publisher) WriteAVCCAudio(ts uint32, frame *util.BLL, pool util.BytesPool) {
	if frame.ByteLength < 4 {
		return
	}
	if p.AudioTrack == nil {
		b0 := frame.GetByte(0)
		switch codecID := codec.AudioCodecID(b0 >> 4); codecID {
		case codec.CodecID_AAC:
			if frame.GetByte(1) != 0 {
				return
			}
			a := track.NewAAC(p.Stream, pool)
			p.AudioTrack = a
			a.AVCCHead = []byte{frame.GetByte(0), 1}
			a.WriteAVCC(0, frame)
		case codec.CodecID_PCMA,
			codec.CodecID_PCMU:
			alaw := true
			if codecID == codec.CodecID_PCMU {
				alaw = false
			}
			a := track.NewG711(p.Stream, alaw, pool)
			p.AudioTrack = a
			a.Audio.SampleRate = uint32(codec.SoundRate[(b0&0x0c)>>2])
			if b0&0x02 == 0 {
				a.Audio.SampleSize = 8
			}
			a.Channels = b0&0x01 + 1
			a.AVCCHead = []byte{b0}
			a.WriteAVCC(ts, frame)
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
	Pull() error
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
