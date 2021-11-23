package engine

import (
	"time"

	"github.com/Monibuca/utils/v3"
	"github.com/Monibuca/utils/v3/codec"
)

type RTPAudio struct {
	RTPDemuxer `json:"-"`
	*AudioTrack
}

func (s *Stream) NewRTPAudio(codec byte) (r *RTPAudio) {
	r = &RTPAudio{
		AudioTrack: s.NewAudioTrack(codec),
	}
	r.OnDemux = r.push
	return
}

// 该函数只执行一次
func (v *RTPAudio) push(ts uint32, payload []byte) {
	switch v.CodecID {
	case codec.CodecID_AAC:
		v.OnDemux = func(ts uint32, payload []byte) {
			for _, payload := range codec.ParseRTPAAC(payload) {
				v.PushRaw(ts, payload)
			}
		}
	case codec.CodecID_PCMA, codec.CodecID_PCMU:
		v.OnDemux = func(ts uint32, payload []byte) {
			v.PushRaw(ts, payload)
		}
	default:
		utils.Println("RTP Publisher: Unsupported codec", v.CodecID)
		return // TODO
	}
	v.timeBase = uint64(v.AudioTrack.SoundRate)
	v.timestamp = time.Now()
	v.OnDemux(ts, payload)
}
