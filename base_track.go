package engine

import "github.com/pion/rtp"

type Track interface {
	PushRTP(rtp.Packet)
	GetBPS(int)
	Dispose()
}
// 一定要在写入Track的协程中调用该函数，这个函数的作用是防止订阅者无限等待
func DisposeTracks(tracks ...Track) {
	for _, track := range tracks {
		if track != nil {
			track.Dispose()
		}
	}
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
func (t *Track_Audio) Dispose() {
	t.Buffer.Current.Done()
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
func (t *Track_Video) Dispose() {
	t.Buffer.Current.Done()
}
