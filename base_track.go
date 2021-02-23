package engine

import "github.com/pion/rtp"

type Track interface {
	PushRTP(rtp.Packet)
}
type Track_Audio struct {
	Buffer      *Ring_Audio `json:"-"`
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
	Buffer      *Ring_Video `json:"-"`
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
