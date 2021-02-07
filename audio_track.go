package engine

import (
	"context"
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
	ASC         []byte //audio special configure
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
func (at *AudioTrack) Play(ctx context.Context, callback func(AudioPack)) {
	ring := at.Buffer.SubRing(at.Buffer.Index)
	ring.Current.Wait()
	droped := 0
	var action, send func()
	drop := func() {
		if at.Buffer.Index-ring.Index < 10 {
			action = send
		} else {
			droped++
		}
	}
	send = func() {
		callback(ring.Current.AudioPack)

		//s.BufferLength = pIndex - ring.Index
		//s.Delay = s.AVRing.Timestamp - packet.Timestamp
		if at.Buffer.Index-ring.Index > 128 {
			action = drop
		}
	}
	for action = send; ; ring.NextR() {
		select {
		case <-ctx.Done():
			return
		default:
			action()
		}
	}
}
