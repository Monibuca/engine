package engine

import (
	"sort"

	"github.com/Monibuca/utils/v3/codec"
	"github.com/pion/rtp"
)

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
	if t < vt.Buffer.GetLast().Timestamp {
		if vt.WaitIDR.Err() == nil {
			return
		}
		//有B帧
		var tmpVT VideoTrack
		tmpVT.Buffer = NewRing_Video()
		tmpVT.revIDR = func() {
			tmpVT.IDRIndex = tmpVT.Buffer.Index
		}
		// tmpVT.pushRTP = func(p rtp.Packet) {
		// 	tmpVT.Push(VideoPack{Timestamp:p.Timestamp/90,Payload:p.Payload})
		// }
		gopBuffer := tmpVT.Buffer //缓存一个GOP用来计算dts
		var gopFirst byte
		var tsSlice TSSlice
		for i := vt.IDRIndex; vt.Buffer.Index != i; i++ {
			t := vt.Buffer.GetAt(i)
			c := gopBuffer.Current
			c.VideoPack = t.VideoPack.Clone()
			tsSlice = append(tsSlice, gopBuffer.Current.Timestamp)
			gopBuffer.NextW()
		}
		v.Push = func(payload []byte) {
			if err := v.Unmarshal(payload); err != nil {
				return
			}
			t := v.Timestamp / 90
			c := gopBuffer.Current
			vp := VideoPack{Timestamp: t, NALUs: [][]byte{v.Payload}}
			tmpVT.PushNalu(vp)
			if c != gopBuffer.Current {
				if c.IDR {
					sort.Sort(tsSlice) //排序后相当于DTS列表
					var offset uint32
					for i := 0; i < len(tsSlice); i++ {
						j := gopFirst + byte(i)
						f := gopBuffer.GetAt(j)
						if f.Timestamp+offset < tsSlice[i] {
							offset = tsSlice[i] - f.Timestamp
						}
					}
					for i := 0; i < len(tsSlice); i++ {
						f := gopBuffer.GetAt(gopFirst + byte(i))
						f.CompositionTime = f.Timestamp + offset - tsSlice[i]
						f.Timestamp = tsSlice[i]
						vt.PushNalu(f.VideoPack)
					}
					gopFirst = gopBuffer.Index - 1
					tsSlice = nil
				}
				tsSlice = append(tsSlice, t)
			}
		}
		v.Push(payload)
		return
	}
	vt.PushNalu(VideoPack{Timestamp: t, NALUs: [][]byte{v.Payload}})
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
				at.PushRaw(AudioPack{Timestamp: v.Timestamp / 90, Raw: payload})
			}
		}
	case 7, 8:
		v.Push = func(payload []byte) {
			if err := v.Unmarshal(payload); err != nil {
				return
			}
			at.PushRaw(AudioPack{Timestamp: v.Timestamp / 8, Raw: v.Payload})
		}
	}
}
