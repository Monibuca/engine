package engine

import (
	"github.com/Monibuca/utils/v3/codec"
	"github.com/pion/rtp"
)

type TSSlice []uint32

func (s TSSlice) Len() int { return len(s) }

func (s TSSlice) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

func (s TSSlice) Less(i, j int) bool { return s[i] < s[j] }

type RTPPublisher struct {
	rtp.Packet
	Push func(payload []byte)
}
type RTPAudio struct {
	RTPPublisher
	*AudioTrack
}
type RTPVideo struct {
	RTPPublisher
	*VideoTrack
}

func (s *Stream) NewRTPVideo(codec byte) (r *RTPVideo) {
	r = &RTPVideo{
		VideoTrack: s.NewVideoTrack(codec),
	}
	r.Push = r.push
	return
}

func (s *Stream) NewRTPAudio(codec byte) (r *RTPAudio) {
	r = &RTPAudio{
		AudioTrack: s.NewAudioTrack(codec),
	}
	r.Push = r.push
	return
}

func (v *RTPVideo) push(payload []byte) {
	vt := v.VideoTrack
	if err := v.Unmarshal(payload); err != nil {
		return
	}
	t := v.Timestamp / 90
	if t < vt.Prev().Value.(*VideoPack).Timestamp {
		if vt.WaitIDR.Err() == nil {
			return
		}
		//有B帧
		tmpVT := v.Stream.NewVideoTrack(0)
		tmpVT.CodecID = v.CodecID
		tmpVT.revIDR = func() {
			if tmpVT.lastIDR != nil {
				//TODO: 排序
			}
			tmpVT.IDRing = tmpVT.Ring
			tmpVT.lastIDR = tmpVT.CurrentValue().(*VideoPack)
		}
		v.Push = func(payload []byte) {
			if err := v.Unmarshal(payload); err != nil {
				return
			}
			tmpVT.PushNalu(VideoPack{BasePack: BasePack{Timestamp: v.Timestamp / 90}, NALUs: [][]byte{v.Payload}})
		}
		v.Push(payload)
		return
	}
	vt.PushNalu(VideoPack{BasePack: BasePack{Timestamp: t}, NALUs: [][]byte{v.Payload}})
}
func (v *RTPAudio) push(payload []byte) {
	at := v.AudioTrack
	if err := v.Unmarshal(payload); err != nil {
		return
	}
	switch at.CodecID {
	case 10:
		v.Push = func(payload []byte) {
			if err := v.Unmarshal(payload); err != nil {
				return
			}
			for _, payload = range codec.ParseRTPAAC(v.Payload) {
				at.PushRaw(AudioPack{BasePack: BasePack{Timestamp: v.Timestamp / 8}, Raw: payload})
			}
		}
	case 7, 8:
		v.Push = func(payload []byte) {
			if err := v.Unmarshal(payload); err != nil {
				return
			}
			at.PushRaw(AudioPack{BasePack: BasePack{Timestamp: v.Timestamp / 8}, Raw: v.Payload})
		}
	}
}
