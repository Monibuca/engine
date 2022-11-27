package track

import (
	"net"

	"m7s.live/engine/v4/codec"
	. "m7s.live/engine/v4/common"
)

var adcflv1 = []byte{codec.FLV_TAG_TYPE_AUDIO, 0, 0, 4, 0, 0, 0, 0, 0, 0, 0}
var adcflv2 = []byte{0, 0, 0, 15}

type Audio struct {
	Media[AudioSlice]
	CodecID    codec.AudioCodecID
	Channels   byte
	SampleSize byte
	AVCCHead   []byte // 音频包在AVCC格式中，AAC会有两个字节，其他的只有一个字节
	// Profile:
	// 0: Main profile
	// 1: Low Complexity profile(LC)
	// 2: Scalable Sampling Rate profile(SSR)
	// 3: Reserved
	Profile byte
}

// 为json序列化而计算的数据
func (a *Audio) SnapForJson() {
	v := a.LastValue
	if a.RawPart != nil {
		a.RawPart = a.RawPart[:0]
	}
	a.RawSize = 0
	for i := 0; i < len(v.Raw) && i < 10; i++ {
		l := len(v.Raw[i])
		a.RawSize += l
		if sl := len(a.RawPart); sl < 10 {
			for j := 0; j < l && j < 10-sl; j++ {
				a.RawPart = append(a.RawPart, int(v.Raw[i][j]))
			}
		}
	}
}

func (a *Audio) IsAAC() bool {
	return a.CodecID == codec.CodecID_AAC
}
func (a *Audio) GetDecConfSeq() int {
	return a.DecoderConfiguration.Seq
}
func (a *Audio) Attach() {
	a.Stream.AddTrack(a)
	a.Attached = 1
}
func (a *Audio) Detach() {
	a.Stream.RemoveTrack(a)
	a.Attached = 2
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
	a.Profile = ((adts[2] & 0xc0) >> 6) + 1
	sampleRate := (adts[2] & 0x3c) >> 2
	channel := ((adts[2] & 0x1) << 2) | ((adts[3] & 0xc0) >> 6)
	config1 := (a.Profile << 3) | ((sampleRate & 0xe) >> 1)
	config2 := ((sampleRate & 0x1) << 7) | (channel << 3)
	a.SampleRate = uint32(codec.SamplingFrequencies[sampleRate])
	a.Channels = channel
	avcc := []byte{0xAF, 0x00, config1, config2}
	a.DecoderConfiguration = DecoderConfiguration[AudioSlice]{
		97,
		net.Buffers{avcc},
		avcc[2:],
		0,
	}
	a.Attach()
}

func (av *Audio) WriteRaw(pts uint32, raw AudioSlice) {
	curValue := &av.Value
	curValue.BytesIn += len(raw)
	if len(av.AVCCHead) == 2 {
		raw = raw[7:] //AAC 去掉7个字节的ADTS头
	}
	av.WriteSlice(raw)
	curValue.DTS = pts
	curValue.PTS = pts
	av.Flush()
}

func (av *Audio) WriteAVCC(ts uint32, frame AVCCFrame) {
	curValue := &av.AVRing.RingBuffer.Value
	curValue.BytesIn += len(frame)
	curValue.AppendAVCC(frame)
	curValue.DTS = ts * 90
	curValue.PTS = curValue.DTS
}

func (a *Audio) Flush() {
	// AVCC 格式补完
	value := &a.Value
	if a.ComplementAVCC() {
		value.AppendAVCC(a.AVCCHead)
		for _, raw := range value.Raw {
			value.AppendAVCC(raw)
		}
	}
	if a.ComplementRTP() {
		var packet = make(net.Buffers, len(value.Raw))
		for i, raw := range value.Raw {
			packet[i] = raw
		}
		a.PacketizeRTP(packet)
	}
	a.Media.Flush()
}
