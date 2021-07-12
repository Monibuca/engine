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
	t0 := v.Timestamp
	v.Push = func(payload []byte) {
		if err := v.Unmarshal(payload); err != nil {
			return
		}
		t1 := (v.Timestamp - t0)/90
		if t1 < vt.Prev().Value.(*RingItem).Value.(*VideoPack).Timestamp {
			if vt.WaitIDR.Err() == nil {
				return
			}
			var ts TSSlice
			//有B帧
			tmpVT := v.Stream.NewVideoTrack(0)
			tmpVT.ExtraData = v.ExtraData
			tmpVT.CodecID = v.CodecID
			tmpVT.revIDR = func() {
				l := ts.Len()
				sort.Sort(ts)
				start := tmpVT.Move(-l)
				for i := 0; i < l; i++ {
					vp := start.Value.(*RingItem).Value.(*VideoPack)
					pack := vt.current()
					pack.IDR = vp.IDR
					pack.Timestamp = ts[i]
					pack.CompositionTime = vp.Timestamp - ts[i]
					pack.NALUs = vp.NALUs
					vt.push(pack)
					start = start.Next()
				}
				ts = nil
			}
			v.Push = func(payload []byte) {
				if err := v.Unmarshal(payload); err != nil {
					return
				}
				r := tmpVT.Ring
				t := (v.Timestamp - t0) / 90
				if tmpVT.PushNalu(t, 0, v.Payload); r != tmpVT.Ring {
					ts = append(ts, t)
				}
			}
			v.Push(payload)
			return
		}
		vt.PushNalu(t1, 0, v.Payload)
	}
	v.Push(payload)
}
func (v *RTPAudio) push(payload []byte) {
	at := v.AudioTrack
	if err := v.Unmarshal(payload); err != nil {
		return
	}
	startTime := v.Timestamp
	switch at.CodecID {
	case 10:
		v.Push = func(payload []byte) {
			if err := v.Unmarshal(payload); err != nil {
				return
			}
			for _, payload = range codec.ParseRTPAAC(v.Payload) {
				at.PushRaw((v.Timestamp-startTime)/90, payload)
			}
		}
	case 7, 8:
		v.Push = func(payload []byte) {
			if err := v.Unmarshal(payload); err != nil {
				return
			}
			at.PushRaw((v.Timestamp-startTime)/8, v.Payload)
		}
	}
	v.Push(payload)
}
