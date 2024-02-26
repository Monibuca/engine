package track

import (
	"github.com/pkg/errors"
	"m7s.live/engine/v4/codec"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/util"
)

var _ SpesificTrack = (*Opus)(nil)

func NewOpus(puber IPuber, stuff ...any) (opus *Opus) {
	opus = &Opus{}
	opus.CodecID = codec.CodecID_OPUS
	opus.SampleSize = 16
	opus.Channels = 2
	opus.AVCCHead = []byte{(byte(opus.CodecID) << 4) | (1 << 1)}
	opus.SetStuff("opus", uint32(48000), byte(111), opus, stuff, puber)
	if opus.BytesPool == nil {
		opus.BytesPool = make(util.BytesPool, 17)
	}
	go opus.Attach()
	return
}

type Opus struct {
	Audio
}

func (opus *Opus) WriteAVCC(ts uint32, frame *util.BLL) error {
	return errors.New("opus not support WriteAVCC")
}

func (opus *Opus) WriteRTPFrame(rtpItem *util.ListItem[RTPFrame]) {
	frame := &rtpItem.Value
	opus.Value.RTP.Push(rtpItem)
	if opus.SampleRate != 90000 {
		opus.generateTimestamp(uint32(uint64(frame.Timestamp) * 90000 / uint64(opus.SampleRate)))
	}
	opus.AppendAuBytes(frame.Payload)
	opus.Flush()
}

func (opus *Opus) CompleteRTP(value *AVFrame) {
	if value.AUList.ByteLength > RTPMTU {
		var packets [][][]byte
		r := value.AUList.Next.Value.NewReader()
		for bufs := r.ReadN(RTPMTU); len(bufs) > 0; bufs = r.ReadN(RTPMTU) {
			packets = append(packets, bufs)
		}
		opus.PacketizeRTP(packets...)
	} else {
		opus.Audio.CompleteRTP(value)
	}
}
