package engine

import (
	"sort"

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
	if t < vt.Prev().Value.(*RingItem).Value.(*VideoPack).Timestamp {
		if vt.WaitIDR.Err() == nil {
			return
		}
		var ts TSSlice
		//有B帧
		tmpVT := v.Stream.NewVideoTrack(0)
		tmpVT.CodecID = v.CodecID
		tmpVT.revIDR = func() {
			l := len(ts)
			sort.Sort(ts)
			start := tmpVT.Move(-l)
			for i := 0; i < l; i++ {
				vp := start.Value.(*RingItem).Value.(*VideoPack)
				pts := vp.Timestamp
				vp.Timestamp = ts[i]
				vp.CompositionTime = pts - ts[i]
				vt.push(*vp)
				start = start.Next()
			}
			ts = nil
		}
		v.Push = func(payload []byte) {
			if err := v.Unmarshal(payload); err != nil {
				return
			}
			r := tmpVT.Ring
			t := v.Timestamp / 90
			if tmpVT.PushNalu(VideoPack{BasePack: BasePack{Timestamp: t}, NALUs: [][]byte{v.Payload}}); r != tmpVT.Ring {
				ts = append(ts, t)
			}
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
