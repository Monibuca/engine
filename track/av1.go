package track

import (
	"m7s.live/engine/v4/codec"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/util"
)

var _ SpesificTrack = (*AV1)(nil)

type AV1 struct {
	Video
}

func NewAV1(stream IStream, stuff ...any) (vt *AV1) {
	vt = &AV1{}
	vt.Video.CodecID = codec.CodecID_AV1
	vt.SetStuff("av1", byte(96), uint32(90000), vt, stuff, stream)
	if vt.BytesPool == nil {
		vt.BytesPool = make(util.BytesPool, 17)
	}
	vt.nalulenSize = 4
	vt.dtsEst = NewDTSEstimator()
	return
}
