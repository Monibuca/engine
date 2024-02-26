package track

import (
	"io"

	"go.uber.org/zap"
	"m7s.live/engine/v4/codec"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/util"
)

var _ SpesificTrack = (*G711)(nil)

func NewG711(puber IPuber, alaw bool, stuff ...any) (g711 *G711) {
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
	g711.SetStuff(uint32(8000), g711, stuff, puber)
	if g711.BytesPool == nil {
		g711.BytesPool = make(util.BytesPool, 17)
	}
	go g711.Attach()
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
	i := 0
	frame.Range(func(v util.Buffer) bool {
		if i == 0 {
			v = v.SubBuf(1, v.Len()-1)
		}
		g711.Value.AUList.Push(g711.BytesPool.GetShell(v))
		i++
		return true
	})
	g711.Audio.WriteAVCC(ts, frame)
	return nil
}

func (g711 *G711) WriteRTPFrame(rtpItem *util.ListItem[RTPFrame]) {
	frame := &rtpItem.Value
	g711.Value.RTP.Push(rtpItem)
	if g711.SampleRate != 90000 {
		g711.generateTimestamp(uint32(uint64(frame.Timestamp) * 90000 / uint64(g711.SampleRate)))
	}
	g711.AppendAuBytes(frame.Payload)
	g711.Flush()
}

func (g711 *G711) CompleteRTP(value *AVFrame) {
	if value.AUList.ByteLength > RTPMTU {
		var packets [][][]byte
		r := value.AUList.Next.Value.NewReader()
		for bufs := r.ReadN(RTPMTU); len(bufs) > 0; bufs = r.ReadN(RTPMTU) {
			packets = append(packets, bufs)
		}
		g711.PacketizeRTP(packets...)
	} else {
		g711.Audio.CompleteRTP(value)
	}
}
