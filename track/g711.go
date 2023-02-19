package track

import (
	"io"
	"time"

	"go.uber.org/zap"
	"m7s.live/engine/v4/codec"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/util"
)

var _ SpesificTrack = (*G711)(nil)

func NewG711(stream IStream, alaw bool, stuff ...any) (g711 *G711) {
	g711 = &G711{}
	if alaw {
		g711.Name = "pcma"
		g711.PayloadType = 8
	} else {
		g711.Name = "pcmu"
		g711.PayloadType = 0
	}
	if alaw {
		g711.CodecID = codec.CodecID_PCMA
	} else {
		g711.CodecID = codec.CodecID_PCMU
	}
	g711.SampleSize = 8
	g711.Channels = 1
	g711.AVCCHead = []byte{(byte(g711.CodecID) << 4) | (1 << 1)}
	g711.SetStuff(stream, int(32), uint32(8000), g711, time.Millisecond*10)
	g711.SetStuff(stuff...)
	g711.Attach()
	return
}

type G711 struct {
	Audio
}

func (g711 *G711) WriteAVCC(ts uint32, frame *util.BLL) error {
	if l := frame.ByteLength; l < 2 {
		g711.Error("AVCC data too short", zap.Int("len", l))
		return io.ErrShortWrite
	}
	g711.Value.AUList.Push(g711.BytesPool.GetShell(frame.Next.Value[1:]))
	frame.Range(func(v util.Buffer) bool {
		g711.Value.AUList.Push(g711.BytesPool.GetShell(v))
		return true
	})
	g711.Audio.WriteAVCC(ts, frame)
	return nil
}

func (g711 *G711) WriteRTPFrame(frame *RTPFrame) {
	g711.AppendAuBytes(frame.Payload)
}
