package track

import (
	"time"

	"github.com/pion/rtp"
	"m7s.live/engine/v4/codec"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/config"
)

func NewG711(stream IStream, alaw bool) (g711 *G711) {
	g711 = &G711{}
	if alaw {
		g711.Name = "pcma"
	} else {
		g711.Name = "pcmu"
	}
	g711.Stream = stream
	if alaw {
		g711.CodecID = codec.CodecID_PCMA
	} else {
		g711.CodecID = codec.CodecID_PCMU
	}
	g711.Init(32)
	g711.Poll = time.Millisecond * 10
	g711.DecoderConfiguration.PayloadType = 97
	if config.Global.RTPReorder {
		g711.orderQueue = make([]*RTPFrame, 20)
	}
	g711.SampleSize = 8
	g711.SampleRate = 8000
	g711.Attach()
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
	g711.Value.AppendRTP(frame)
	if frame.Marker {
		g711.generateTimestamp()
		g711.Flush()
	}
}
