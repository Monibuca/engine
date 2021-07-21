package engine

import (
	"github.com/Monibuca/utils/v3"
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
	var p *VideoPack
	t0 := v.Timestamp
	tmpVT := v.Stream.NewVideoTrack(0)
	tmpVT.ExtraData = v.ExtraData
	tmpVT.CodecID = v.CodecID
	start := tmpVT.Ring
	tmpVT.PushNalu(0, 0, v.Payload)
	v.Push = func(payload []byte) {
		if err := v.Unmarshal(payload); err != nil {
			return
		}
		t1 := (v.Timestamp - t0) / 90
		utils.Println("video:", t1)
		tmpVT.PushNalu(t1, 0, v.Payload)
		end := tmpVT.Prev()
		if start != end {
			for next := start; next != end; next = next.Next() {
				vp := next.Value.(*RingItem).Value.(*VideoPack)
				vpNext := next.Next().Value.(*RingItem).Value.(*VideoPack)
				lastB := false
				if p != nil && p.Timestamp < vpNext.Timestamp {
					lastB = true
				}
				if vp.Timestamp > vpNext.Timestamp {
					p = vp
				}
				pack := vt.current()
				if p != nil {
					if lastB {
						pack.Timestamp = p.Timestamp
						p = nil
					} else {
						pack.Timestamp = vpNext.Timestamp
					}
					pack.CompositionTime = vp.Timestamp - pack.Timestamp
				} else {
					pack.Timestamp = vp.Timestamp
				}
				pack.NALUs = vp.NALUs
				pack.IDR = vp.IDR
				vt.push(pack)
			}
			start = end
		}
		// if t1 < vt.Prev().Value.(*RingItem).Value.(*VideoPack).Timestamp {
		// 	if vt.WaitIDR.Err() == nil {
		// 		return
		// 	}
		// 	var buffer, pool = list.New(), list.New()
		// 	var ts TSSlice
		// 	//有B帧
		// 	tmpVT := v.Stream.NewVideoTrack(0)
		// 	tmpVT.ExtraData = v.ExtraData
		// 	tmpVT.CodecID = v.CodecID
		// 	tmpVT.revIDR = func() {
		// 		l := ts.Len()
		// 		sort.Sort(ts)
		// 		start := tmpVT.Move(-l)
		// 		for i := 0; i < l; i++ {
		// 			vp := start.Value.(*RingItem).Value.(*VideoPack)
		// 			var pack *VideoPack
		// 			if pool.Len() > 0 {
		// 				pack = pool.Remove(pool.Front()).(*VideoPack)
		// 			} else {
		// 				pack = &VideoPack{}
		// 			}
		// 			pack.IDR = vp.IDR
		// 			pack.Timestamp = ts[i]
		// 			pack.CompositionTime = vp.Timestamp - ts[i]
		// 			pack.NALUs = vp.NALUs
		// 			buffer.PushBack(pack)
		// 			start = start.Next()
		// 		}
		// 		ts = ts[:0]
		// 	}
		// 	v.Push = func(payload []byte) {
		// 		if err := v.Unmarshal(payload); err != nil {
		// 			return
		// 		}
		// 		r := tmpVT.Ring
		// 		t := (v.Timestamp - t0) / 90
		// 		if tmpVT.PushNalu(t, 0, v.Payload); r != tmpVT.Ring {
		// 			ts = append(ts, t)
		// 			if buffer.Len() > 0 {
		// 				vp := buffer.Remove(buffer.Front()).(*VideoPack)
		// 				pack := vt.current()
		// 				pack.IDR = vp.IDR
		// 				pack.Timestamp = vp.Timestamp
		// 				pack.CompositionTime = vp.CompositionTime
		// 				pack.NALUs = vp.NALUs
		// 				vt.push(pack)
		// 				pool.PushBack(vp)
		// 			}
		// 		}
		// 	}
		// 	v.Push(payload)
		// 	return
		// }
		//vt.PushNalu(t1, 0, v.Payload)
	}
	//v.Push(payload)
}
func (v *RTPAudio) push(payload []byte) {
	at := v.AudioTrack
	if err := v.Unmarshal(payload); err != nil {
		return
	}
	t0 := v.Timestamp
	switch at.CodecID {
	case 10:
		v.Push = func(payload []byte) {
			if err := v.Unmarshal(payload); err != nil {
				return
			}
			t1 := (v.Timestamp - t0) / 90
			utils.Println("audio:", t1)
			for _, payload = range codec.ParseRTPAAC(v.Payload) {
				at.PushRaw(t1, payload)
			}
		}
	case 7, 8:
		v.Push = func(payload []byte) {
			if err := v.Unmarshal(payload); err != nil {
				return
			}
			at.PushRaw((v.Timestamp-t0)/8, v.Payload)
		}
	}
	v.Push(payload)
}
