package track

import (
	"time"

	"github.com/pion/rtp"
	"m7s.live/engine/v4/codec"
	. "m7s.live/engine/v4/common"
)

func NewG711(stream IStream, alaw bool) (g711 *G711) {
	g711 = &G711{}
	if alaw {
		g711.Audio.Name = "pcma"
	} else {
		g711.Audio.Name = "pcmu"
	}
	g711.Audio.Stream = stream
	if alaw {
		g711.Audio.CodecID = codec.CodecID_PCMA
	} else {
		g711.Audio.CodecID = codec.CodecID_PCMU
	}
	g711.Audio.Init(32)
	g711.Audio.Media.Poll = time.Millisecond * 10
	g711.Audio.DecoderConfiguration.PayloadType = 97
	g711.Audio.SampleSize = 8
	g711.Audio.SampleRate = 8000
	g711.Audio.Attach()
	return
}

type G711 struct {
	Audio
}

// WriteRTPPack 写入已反序列化的RTP包
func (g711 *G711) WriteRTPPack(p *rtp.Packet) {
	for frame := g711.UnmarshalRTPPacket(p); frame != nil; frame = g711.nextRTPFrame() {
		g711.writeRTPFrame(frame)
	}
}

// WriteRTP 写入未反序列化的RTP包
func (g711 *G711) WriteRTP(raw []byte) {
	for frame := g711.UnmarshalRTP(raw); frame != nil; frame = g711.nextRTPFrame() {
		g711.writeRTPFrame(frame)
	}
}

func (g711 *G711) WriteAVCC(ts uint32, frame AVCCFrame) {
	g711.WriteSlice(AudioSlice(frame[1:]))
	g711.Audio.WriteAVCC(ts, frame)
	g711.Flush()
}

func (g711 *G711) writeRTPFrame(frame *RTPFrame) {
	g711.WriteSlice(frame.Payload)
	g711.Audio.Media.AVRing.RingBuffer.Value.AppendRTP(frame)
	if frame.Marker {
		g711.generateTimestamp()
		g711.Flush()
	}
}
