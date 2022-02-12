package track

import (
	"time"

	"github.com/Monibuca/engine/v4/codec"
	. "github.com/Monibuca/engine/v4/common"
)

func NewG711(stream IStream, alaw bool) (g711 *G711) {
	g711 = &G711{}
	g711.Stream = stream
	if alaw {
		g711.CodecID = codec.CodecID_PCMA
	} else {
		g711.CodecID = codec.CodecID_PCMU
	}
	g711.Init(stream, 32)
	g711.Poll = time.Millisecond * 20
	g711.DecoderConfiguration.PayloadType = 97
	return
}

type G711 Audio

func (g711 *G711) WriteAVCC(ts uint32, frame AVCCFrame) {
	g711.WriteSlice(AudioSlice(frame[1:]))
	(*Audio)(g711).WriteAVCC(ts, frame)
	g711.Flush()
}

func (g711 *G711) WriteRTP(raw []byte) {
	var packet RTPFrame
	if frame := packet.Unmarshal(raw); frame == nil {
		return
	}
	g711.WriteSlice(packet.Payload)
	g711.Value.AppendRTP(packet)
	if packet.Marker {
		g711.Flush()
	}
}
