package avformat

import (
	"sync"
)

var (
	SendPacketPool = &sync.Pool{
		New: func() interface{} {
			return new(SendPacket)
		},
	}
)

// Video or Audio
type AVPacket struct {
	Timestamp      uint32
	Type           byte //8 audio,9 video
	IsSequence     bool //序列帧
	IsKeyFrame bool//是否为关键帧
	Payload        []byte
	Number         int //编号，audio和video独立编号
}

func (av *AVPacket) ADTS2ASC() (tagPacket *AVPacket) {
	tagPacket = NewAVPacket(FLV_TAG_TYPE_AUDIO)
	tagPacket.Payload = ADTSToAudioSpecificConfig(av.Payload)
	tagPacket.IsSequence = true
	ADTSLength := 7 + ((1 - int(av.Payload[1]&1)) << 1)
	if len(av.Payload) > ADTSLength {
		av.Payload[0] = 0xAF
		av.Payload[1] = 0x01 //raw AAC
		copy(av.Payload[2:], av.Payload[ADTSLength:])
		av.Payload = av.Payload[:(len(av.Payload) - ADTSLength + 2)]
	}
	return
}

func NewAVPacket(avType byte) (p *AVPacket) {
	p = new(AVPacket)
	p.Type = avType
	return
}
func (av AVPacket) Clone() *AVPacket {
	return &av
}

type SendPacket struct {
	*AVPacket
	Timestamp uint32
}

func (packet *SendPacket) Recycle() {
	SendPacketPool.Put(packet)
}
func NewSendPacket(p *AVPacket, timestamp uint32) (result *SendPacket) {
	result = SendPacketPool.Get().(*SendPacket)
	result.AVPacket = p
	result.Timestamp = timestamp
	return
}
