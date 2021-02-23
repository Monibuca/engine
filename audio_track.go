package engine

import (
	"github.com/Monibuca/utils/v3"
	"github.com/Monibuca/utils/v3/codec"
	"github.com/pion/rtp"
)

type AudioPack struct {
	Timestamp      uint32
	Payload        []byte
	SequenceNumber uint16
}
type AudioTrack struct {
	Track_Audio
	SoundFormat byte   //4bit
	SoundRate   int    //2bit
	SoundSize   byte   //1bit
	SoundType   byte   //1bit
	RtmpTag     []byte //rtmp协议需要先发这个帧
}

func (at *AudioPack) ToRTMPTag(aac byte) []byte {
	audio := at.Payload
	l := len(audio) + 1
	if aac != 0 {
		l++
	}
	payload := utils.GetSlice(l)
	payload[0] = aac
	if aac != 0 {
		payload[1] = 1
		copy(payload[2:], audio)
	} else {
		copy(payload[1:], audio)
	}
	return payload
}

// Push 来自发布者推送的音频
func (at *AudioTrack) Push(timestamp uint32, payload []byte) {
	payloadLen := len(payload)
	if payloadLen < 4 {
		return
	}
	audio := at.Buffer
	audio.Current.Timestamp = timestamp
	audio.Current.Payload = payload
	at.Track_Audio.GetBPS(payloadLen)
	audio.NextW()
}
func (at *AudioTrack) PushRTP(pack rtp.Packet) {
	t := pack.Timestamp / 90
	for _, payload := range codec.ParseRTPAAC(pack.Payload) {
		at.Push(t, payload)
	}
}
func NewAudioTrack() *AudioTrack {
	var result AudioTrack
	result.Buffer = NewRing_Audio()
	return &result
}
func (at *AudioTrack) SetASC(asc []byte) {
	at.RtmpTag = append([]byte{0xAF, 0}, asc...)
	config1 := asc[0]
	config2 := asc[1]
	at.SoundFormat = 10
	//audioObjectType = (config1 & 0xF8) >> 3
	// 1 AAC MAIN 	ISO/IEC 14496-3 subpart 4
	// 2 AAC LC 	ISO/IEC 14496-3 subpart 4
	// 3 AAC SSR 	ISO/IEC 14496-3 subpart 4
	// 4 AAC LTP 	ISO/IEC 14496-3 subpart 4
	at.SoundRate = codec.SamplingFrequencies[((config1&0x7)<<1)|(config2>>7)]
	at.SoundType = (config2 >> 3) & 0x0F //声道
	//frameLengthFlag = (config2 >> 2) & 0x01
	//dependsOnCoreCoder = (config2 >> 1) & 0x01
	//extensionFlag = config2 & 0x01
}
