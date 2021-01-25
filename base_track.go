package engine

import (
	"context"
	"github.com/Monibuca/utils/v3/codec"
)

type Track_Audio struct {
	Buffer      *Ring_Audio
	PacketCount int
	CodecID     byte
	BPS         int
	lastIndex   byte
}

func (t *Track_Audio) GetBPS(payloadLen int) {
	t.PacketCount++
	if lastTimestamp := t.Buffer.GetAt(t.lastIndex).Timestamp; lastTimestamp > 0 && lastTimestamp != t.Buffer.Current.Timestamp {
		t.BPS = payloadLen * 1000 / int(t.Buffer.Current.Timestamp-lastTimestamp)
	}
	t.lastIndex = t.Buffer.Index
}

type Track_Video struct {
	Buffer      *Ring_Video
	PacketCount int
	CodecID     byte
	BPS         int
	lastIndex   byte
}

func (t *Track_Video) GetBPS(payloadLen int) {
	t.PacketCount++
	if lastTimestamp := t.Buffer.GetAt(t.lastIndex).Timestamp; lastTimestamp > 0 && lastTimestamp != t.Buffer.Current.Timestamp {
		t.BPS = payloadLen * 1000 / int(t.Buffer.Current.Timestamp-lastTimestamp)
	}
	t.lastIndex = t.Buffer.Index
}

type TrackCP struct {
	Audio *AudioTrack
	Video *VideoTrack
}

func (tcp *TrackCP) Play(ctx context.Context, cba func(AudioPack), cbv func(VideoPack)) {
	vr := tcp.Video.Buffer.SubRing(tcp.Video.FirstScreen)
	ar := tcp.Audio.Buffer.SubRing(tcp.Audio.Buffer.Index)
	vr.Current.Wait()
	ar.Current.Wait()
	dropping := false
	send_audio := func() {
		cba(ar.Current.AudioPack)
		if tcp.Audio.Buffer.Index-ar.Index > 128 {
			dropping = true
		}
	}
	send_video := func() {
		cbv(vr.Current.VideoPack)
		if tcp.Video.Buffer.Index-vr.Index > 128 {
			dropping = true
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		default:
			if ar.Current.Timestamp > vr.Current.Timestamp {
				if !dropping {
					send_video()
				} else if vr.Current.NalType == codec.NALU_IDR_Picture {
					dropping = false
				}
				vr.NextR()
			} else {
				if !dropping {
					send_audio()
				}
				ar.NextR()
			}
		}
	}
}
