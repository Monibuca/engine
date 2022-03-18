package track

import (
	"net"

	"go.uber.org/zap"
	"m7s.live/engine/v4/codec"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/config"
)

var adcflv1 = []byte{codec.FLV_TAG_TYPE_AUDIO, 0, 0, 4, 0, 0, 0, 0, 0, 0, 0}
var adcflv2 = []byte{0, 0, 0, 15}

type Audio struct {
	Media[AudioSlice]
	CodecID  codec.AudioCodecID
	Channels byte
	AVCCHead []byte // 音频包在AVCC格式中，AAC会有两个字节，其他的只有一个字节
}

func (a *Audio) IsAAC() bool {
	return a.CodecID == codec.CodecID_AAC
}

func (a *Audio) Attach() {
	a.Stream.AddTrack(a)
}
func (a *Audio) Detach() {
	a.Stream = nil
	a.Stream.RemoveTrack(a)
}
func (a *Audio) GetName() string {
	if a.Name == "" {
		return a.CodecID.String()
	}
	return a.Name
}
func (a *Audio) GetInfo() *Audio {
	return a
}

func (a *Audio) WriteADTS(adts []byte) {
	profile := ((adts[2] & 0xc0) >> 6) + 1
	sampleRate := (adts[2] & 0x3c) >> 2
	channel := ((adts[2] & 0x1) << 2) | ((adts[3] & 0xc0) >> 6)
	config1 := (profile << 3) | ((sampleRate & 0xe) >> 1)
	config2 := ((sampleRate & 0x1) << 7) | (channel << 3)
	a.SampleRate = uint32(codec.SamplingFrequencies[sampleRate])
	a.Channels = channel
	avcc := []byte{0xAF, 0x00, config1, config2}
	a.DecoderConfiguration = DecoderConfiguration[AudioSlice]{
		97,
		net.Buffers{avcc},
		avcc[:2],
		net.Buffers{adcflv1, avcc, adcflv2},
	}
}

func (a *Audio) Flush() {
	// AVCC 格式补完
	if a.Value.AVCC == nil && (config.Global.EnableAVCC || config.Global.EnableFLV) {
		a.Value.AppendAVCC(a.AVCCHead)
		for _, raw := range a.Value.Raw {
			a.Value.AppendAVCC(raw)
		}
	}
	// FLV tag 补完
	if a.Value.FLV == nil && config.Global.EnableFLV {
		a.Value.FillFLV(codec.FLV_TAG_TYPE_AUDIO, a.Value.AbsTime)
	}
	if a.Value.RTP == nil && config.Global.EnableRTP {
		var o []byte
		for _, raw := range a.Value.Raw {
			o = append(o, raw...)
		}
		a.PacketizeRTP(o)
	}
	a.Media.Flush()
}

type UnknowAudio struct {
	Base
	AudioTrack
}

func (ua *UnknowAudio) GetName() string {
	return ua.Base.GetName()
}

func (ua *UnknowAudio) Flush() {
	ua.AudioTrack.Flush()
}

func (ua *UnknowAudio) WriteAVCC(ts uint32, frame AVCCFrame) {
	if ua.AudioTrack == nil {
		codecID := frame.AudioCodecID()
		if ua.Name == "" {
			ua.Name = codecID.String()
		}
		switch codecID {
		case codec.CodecID_AAC:
			if !frame.IsSequence() {
				return
			}
			a := NewAAC(ua.Stream)
			ua.AudioTrack = a
			a.SampleSize = 16
			a.AVCCHead = []byte{frame[0], 1}
			a.WriteAVCC(0, frame)
		case codec.CodecID_PCMA,
			codec.CodecID_PCMU:
			alaw := true
			if codecID == codec.CodecID_PCMU {
				alaw = false
			}
			a := NewG711(ua.Stream, alaw)
			ua.AudioTrack = a
			a.SampleRate = uint32(codec.SoundRate[(frame[0]&0x0c)>>2])
			a.SampleSize = 16
			if frame[0]&0x02 == 0 {
				a.SampleSize = 8
			}
			a.Channels = frame[0]&0x01 + 1
			a.AVCCHead = frame[:1]
			ua.AudioTrack.WriteAVCC(ts, frame)
		default:
			ua.Stream.Error("audio codec not support yet", zap.Uint8("codecId", uint8(codecID)))
		}
	} else {
		ua.AudioTrack.WriteAVCC(ts, frame)
	}
}
