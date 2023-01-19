package track

import (
	"time"

	"go.uber.org/zap"
	"m7s.live/engine/v4/codec"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/util"
)

func NewG711(stream IStream, alaw bool) (g711 *G711) {
	g711 = &G711{}
	if alaw {
		g711.Name = "pcma"
	} else {
		g711.Name = "pcmu"
	}
	if alaw {
		g711.CodecID = codec.CodecID_PCMA
	} else {
		g711.CodecID = codec.CodecID_PCMU
	}
	g711.SampleSize = 8
	g711.Channels = 1
	g711.AVCCHead = []byte{(byte(g711.CodecID) << 4) | (1 << 1)}
	g711.SetStuff(stream, int(32), byte(97), uint32(8000), g711, time.Millisecond*10)
	g711.Attach()
	return
}

type G711 struct {
	Audio
}

func (g711 *G711) WriteAVCC(ts uint32, frame AVCCFrame) {
	if l := util.SizeOfBuffers(frame); l < 2 {
		g711.Stream.Error("AVCC data too short", zap.Int("len", l))
		return
	}
	g711.Value.AppendRaw(frame[0][1:])
	for _, data := range frame[1:] {
		g711.Value.AppendRaw(data)
	}
	g711.Audio.WriteAVCC(ts, frame)
}

func (g711 *G711) WriteRTPFrame(frame *RTPFrame) {
	g711.Value.AppendRaw(frame.Payload)
}
