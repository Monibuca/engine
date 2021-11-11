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
	demux      func()
}

func (p *RTPPublisher) Push(payload []byte) {
	if p.Unmarshal(payload) == nil {
		if p.lastTs != 0 {
			if p.SequenceNumber != p.lastSeq+1 {
				println("RTP Publisher: SequenceNumber error", p.lastSeq, p.SequenceNumber)
				return
			} else {
				if p.lastTs > p.Timestamp {
					if p.lastTs-p.Timestamp > 100000 {
						p.absTs += (p.Timestamp)
					} else { //B frame
						p.absTs -= (p.lastTs - p.Timestamp)
					}
				} else {
					p.absTs += (p.Timestamp - p.lastTs)
				}
			}
		}
		p.lastTs = p.Timestamp
		p.lastSeq = p.SequenceNumber
		p.demux()
	}
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
	at := v.AudioTrack
	tb := at.SoundRate
	switch at.CodecID {
	case 10:
		v.demux = func() {
			t1 := uint32(uint64(v.absTs) * 1000 / uint64(tb))
			for _, payload := range codec.ParseRTPAAC(v.Payload) {
				at.PushRaw(t1, payload)
			}
		}
	case 7, 8:
		v.demux = func() {
			at.PushRaw(uint32(uint64(v.absTs) * 1000 / uint64(tb)), v.Payload)
		}
	}
	v.demux()
}
