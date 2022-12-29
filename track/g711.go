package track

import (
	"time"

	"go.uber.org/zap"
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
	if alaw {
		g711.Audio.CodecID = codec.CodecID_PCMA
	} else {
		g711.Audio.CodecID = codec.CodecID_PCMU
	}
	g711.Audio.SampleSize = 8
	g711.SetStuff(stream, int(32), byte(97), uint32(8000), g711, time.Millisecond*10)
	g711.Audio.Attach()
	return
}

type G711 struct {
	Audio
}

func (g711 *G711) WriteAVCC(ts uint32, frame AVCCFrame) {
	if len(frame) < 2 {
		g711.Stream.Error("AVCC data too short", zap.ByteString("data", frame))
		return
	}
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
