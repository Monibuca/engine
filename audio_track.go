package engine

import (
	"io"

	"github.com/Monibuca/utils/v3/codec"
)

type AudioPack struct {
	Timestamp      uint32
	Payload        []byte
	Raw            []byte
	SequenceNumber uint16
}

func (ap AudioPack) Copy(ts uint32) AudioPack {
	ap.Timestamp = ap.Timestamp - ts
	return ap
}

type AudioTrack struct {
	Track_Audio
	SoundRate       int                                    //2bit
	SoundSize       byte                                   //1bit
	Channels        byte                                   //1bit
	ExtraData       []byte                                 `json:"-"` //rtmp协议需要先发这个帧
	PushByteStream  func(pack AudioPack)                   `json:"-"`
	PushRaw         func(pack AudioPack)                   `json:"-"`
	WriteByteStream func(writer io.Writer, pack AudioPack) `json:"-"` //使用函数写入，避免申请内存
}

func (at *AudioTrack) pushByteStream(pack AudioPack) {
	at.CodecID = pack.Payload[0] >> 4
	at.WriteByteStream = func(writer io.Writer, pack AudioPack) {
		writer.Write(pack.Payload)
	}
	switch at.CodecID {
	case 10:
		at.Stream.AudioTracks.AddTrack("aac", at)
	case 7:
		at.Stream.AudioTracks.AddTrack("pcma", at)
	case 8:
		at.Stream.AudioTracks.AddTrack("pcmu", at)
	}
	switch at.CodecID {
	case 10:
		if pack.Payload[1] != 0 {
			return
		} else {
			config1, config2 := pack.Payload[2], pack.Payload[3]
			//audioObjectType = (config1 & 0xF8) >> 3
			// 1 AAC MAIN 	ISO/IEC 14496-3 subpart 4
			// 2 AAC LC 	ISO/IEC 14496-3 subpart 4
			// 3 AAC SSR 	ISO/IEC 14496-3 subpart 4
			// 4 AAC LTP 	ISO/IEC 14496-3 subpart 4
			at.SoundRate = codec.SamplingFrequencies[((config1&0x7)<<1)|(config2>>7)]
			at.Channels = ((config2 >> 3) & 0x0F) //声道
			//frameLengthFlag = (config2 >> 2) & 0x01
			//dependsOnCoreCoder = (config2 >> 1) & 0x01
			//extensionFlag = config2 & 0x01
			at.ExtraData = pack.Payload
			at.PushByteStream = func(pack AudioPack) {
				pack.Raw = pack.Payload[2:]
				at.push(pack)
			}
		}
	default:
		at.SoundRate = codec.SoundRate[(pack.Payload[0]&0x0c)>>2] // 采样率 0 = 5.5 kHz or 1 = 11 kHz or 2 = 22 kHz or 3 = 44 kHz
		at.SoundSize = (pack.Payload[0] & 0x02) >> 1              // 采样精度 0 = 8-bit samples or 1 = 16-bit samples
		at.Channels = pack.Payload[0]&0x01 + 1
		at.ExtraData = pack.Payload[:1]
		at.PushByteStream = func(pack AudioPack) {
			payloadLen := len(pack.Payload)
			if payloadLen < 4 {
				return
			}
			pack.Raw = pack.Payload[1:]
			at.push(pack)
		}
		at.PushByteStream(pack)
	}

}
func (at *AudioTrack) pushRaw(pack AudioPack) {
	switch at.CodecID {
	case 10:
		at.WriteByteStream = func(writer io.Writer, pack AudioPack) {
			writer.Write([]byte{at.ExtraData[0], 1})
			writer.Write(pack.Raw)
		}
	default:
		at.WriteByteStream = func(writer io.Writer, pack AudioPack) {
			writer.Write(at.ExtraData[:1])
			writer.Write(pack.Raw)
		}
	}
	at.PushRaw = at.push
	at.push(pack)
}

// Push 来自发布者推送的音频
func (at *AudioTrack) push(pack AudioPack) {
	if at.Stream != nil {
		at.Stream.Update()
	}
	abr := at.Buffer
	audio := abr.Current
	audio.AudioPack = pack
	if at.Stream.prePayload > 0 && len(pack.Payload) == 0 {
		buffer := abr.GetBuffer()
		at.WriteByteStream(buffer, pack)
		audio.AudioPack = pack
		audio.AudioPack.Payload = buffer.Bytes()
	} else {
		audio.AudioPack = pack
	}
	at.bytes += len(pack.Raw)
	at.GetBPS()
	if audio.Timestamp-at.ts > 1000 {
		at.bytes = 0
		at.ts = audio.Timestamp
	}
	abr.NextW()
}

func (s *Stream) NewAudioTrack(codec byte) (at *AudioTrack) {
	at = &AudioTrack{}
	at.CodecID = codec
	at.PushByteStream = at.pushByteStream
	at.PushRaw = at.pushRaw
	at.Stream = s
	at.Buffer = NewRing_Audio()
	switch codec {
	case 10:
		s.AudioTracks.AddTrack("aac", at)
	case 7:
		s.AudioTracks.AddTrack("pcma", at)
	case 8:
		s.AudioTracks.AddTrack("pcmu", at)
	}
	return
}
func (at *AudioTrack) SetASC(asc []byte) {
	at.ExtraData = append([]byte{0xAF, 0}, asc...)
	config1 := asc[0]
	config2 := asc[1]
	at.CodecID = 10
	//audioObjectType = (config1 & 0xF8) >> 3
	// 1 AAC MAIN 	ISO/IEC 14496-3 subpart 4
	// 2 AAC LC 	ISO/IEC 14496-3 subpart 4
	// 3 AAC SSR 	ISO/IEC 14496-3 subpart 4
	// 4 AAC LTP 	ISO/IEC 14496-3 subpart 4
	at.SoundRate = codec.SamplingFrequencies[((config1&0x7)<<1)|(config2>>7)]
	at.Channels = (config2 >> 3) & 0x0F //声道
	//frameLengthFlag = (config2 >> 2) & 0x01
	//dependsOnCoreCoder = (config2 >> 1) & 0x01
	//extensionFlag = config2 & 0x01
	at.Stream.AudioTracks.AddTrack("aac", at)
}
