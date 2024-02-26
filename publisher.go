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
	common.IPuber
	GetPublisher() *Publisher
}

var _ IPublisher = (*Publisher)(nil)

type Publisher struct {
	IO
	Config            *config.Publish
	common.AudioTrack `json:"-" yaml:"-"`
	common.VideoTrack `json:"-" yaml:"-"`
}

func (p *Publisher) Publish(streamPath string, pub common.IPuber) error {
	return p.receive(streamPath, pub)
}

func (p *Publisher) GetPublisher() *Publisher {
	return p
}

// func (p *Publisher) Stop(reason ...zapcore.Field) {
// 	p.IO.Stop(reason...)
// 	p.Stream.Receive(ACTION_PUBLISHCLOSE)
// }

func (p *Publisher) GetAudioTrack() common.AudioTrack {
	return p.AudioTrack
}
func (p *Publisher) GetVideoTrack() common.VideoTrack {
	return p.VideoTrack
}
func (p *Publisher) GetConfig() *config.Publish {
	return p.Config
}
// func (p *Publisher) OnEvent(event any) {
// 	p.IO.OnEvent(event)
// 	switch event.(type) {
// 	case SEclose, SEKick:
// 		p.AudioTrack = nil
// 		p.VideoTrack = nil
// 	}
// }

func (p *Publisher) WriteAVCCVideo(ts uint32, frame *util.BLL, pool util.BytesPool) {
	if frame.ByteLength < 6 {
		return
	}
	if p.VideoTrack == nil {
		b0 := frame.GetByte(0)
		// https://github.com/veovera/enhanced-rtmp/blob/main/enhanced-rtmp-v1.pdf
		if isExtHeader := b0 & 0b1000_0000; isExtHeader != 0 {
			fourCC := frame.GetUintN(1, 4)
			switch fourCC {
			case codec.FourCC_H265_32:
				p.VideoTrack = track.NewH265(p, pool)
				p.VideoTrack.WriteAVCC(ts, frame)
			case codec.FourCC_AV1_32:
				p.VideoTrack = track.NewAV1(p, pool)
				p.VideoTrack.WriteAVCC(ts, frame)
			}
		} else {
			if frame.GetByte(1) == 0 {
				ts = 0
				switch codecID := codec.VideoCodecID(b0 & 0x0F); codecID {
				case codec.CodecID_H264:
					p.VideoTrack = track.NewH264(p, pool)
				case codec.CodecID_H265:
					p.VideoTrack = track.NewH265(p, pool)
				default:
					p.Stream.Error("video codecID not support", zap.Uint8("codeId", uint8(codecID)))
					return
				}
				p.VideoTrack.WriteAVCC(ts, frame)
			} else {
				p.Stream.Warn("need sequence frame")
			}
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
			a := track.NewAAC(p, pool)
			p.AudioTrack = a
			a.AVCCHead = []byte{frame.GetByte(0), 1}
			a.WriteAVCC(0, frame)
		case codec.CodecID_PCMA,
			codec.CodecID_PCMU:
			alaw := true
			if codecID == codec.CodecID_PCMU {
				alaw = false
			}
			a := track.NewG711(p, alaw, pool)
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
