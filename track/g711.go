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
	return
}

type G711 struct {
	BaseAudio
}

func (g711 *G711) WriteAVCC(ts uint32, frame AVCCFrame) {
	g711.Value.AppendRaw(AudioSlice(frame[1:]))
	g711.BaseAudio.WriteAVCC(ts, frame)
}
