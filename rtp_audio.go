package engine

import (
	"github.com/Monibuca/utils/v3/codec"
	"github.com/pion/rtp"
)

type RTPPublisher struct {
	rtp.Packet `json:"-"`
	lastTs     uint32
	absTs      uint32
	lastSeq    uint16
	ts uint32 //毫秒单位的时间戳
	demux      func()
}

type RTPAudio struct {
	RTPPublisher
	*AudioTrack
}

func (s *Stream) NewRTPAudio(codec byte) (r *RTPAudio) {
	r = &RTPAudio{
		AudioTrack: s.NewAudioTrack(codec),
	}
	r.demux = r.push
	return
}

func (v *RTPAudio) push() {
	switch v.CodecID {
	case codec.CodecID_AAC:
		v.demux = func() {
			for _, payload := range codec.ParseRTPAAC(v.Payload) {
				v.PushRaw(v.ts, payload)
			}
		}
	case codec.CodecID_PCMA, codec.CodecID_PCMU:
		v.demux = func() {
			v.PushRaw(v.ts, v.Payload)
		}
	}
	v.demux()
}

func (p *RTPAudio) Push(payload []byte) {
	if p.Unmarshal(payload) == nil {
		if p.lastTs != 0 {
			if p.SequenceNumber != p.lastSeq+1 {
				println("RTP Publisher: SequenceNumber error", p.lastSeq, p.SequenceNumber)
				return
			} else {
				// if p.lastTs > p.Timestamp {
				// 	if p.lastTs-p.Timestamp > 100000 {
				// 		p.absTs += (p.Timestamp)
				// 	} else { //B frame
				// 		p.absTs -= (p.lastTs - p.Timestamp)
				// 	}
				// } else {
				// 	p.absTs += (p.Timestamp - p.lastTs)
				// }
				p.absTs += (p.Timestamp - p.lastTs)
				p.ts = uint32(uint64(p.absTs) * 1000 / uint64(p.SoundRate))
			}
		}
		p.lastTs = p.Timestamp
		p.lastSeq = p.SequenceNumber
		p.demux()
	}
}
