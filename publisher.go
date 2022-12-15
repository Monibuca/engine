package engine

import (
	"go.uber.org/zap"
	"m7s.live/engine/v4/codec"
	"m7s.live/engine/v4/common"
	"m7s.live/engine/v4/config"
	"m7s.live/engine/v4/track"
)

type IPublisher interface {
	IIO
	GetPublisher() *Publisher
	getAudioTrack() common.AudioTrack
	getVideoTrack() common.VideoTrack
}

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
