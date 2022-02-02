package track

import (
	"strings"

	"github.com/Monibuca/engine/v4/codec"
	. "github.com/Monibuca/engine/v4/common"
	"github.com/Monibuca/engine/v4/util"
)

type Video interface {
	AVTrack
	ReadRing() *AVRing[NALUSlice]
	Play(onVideo func(*AVFrame[NALUSlice]) bool)
}

type H264H265 struct {
	Media[NALUSlice]
	IDRing      *util.Ring[AVFrame[NALUSlice]] `json:"-"` //最近的关键帧位置，首屏渲染
	SPSInfo     codec.SPSInfo
	GOP         int //关键帧间隔
	nalulenSize int //avcc格式中表示nalu长度的字节数，通常为4
	idrCount    int //缓存中包含的idr数量
}

func (t *H264H265) ComputeGOP() {
	t.idrCount++
	if t.IDRing != nil {
		t.GOP = int(t.Value.SeqInTrack - t.IDRing.Value.SeqInTrack)
		if l := t.Size - t.GOP - 5; l > 5 {
			t.Size -= l
			//缩小缓冲环节省内存
			t.Unlink(l).Do(func(v AVFrame[NALUSlice]) {
				if v.IFrame {
					t.idrCount--
				}
				v.Reset()
			})
		}
	}
	t.IDRing = t.Ring
}

func (vt *H264H265) WriteAVCC(ts uint32, frame AVCCFrame) {
	vt.Media.WriteAVCC(ts, frame)
	for nalus := frame[5:]; len(nalus) > vt.nalulenSize; {
		nalulen := util.ReadBE[int](nalus[:vt.nalulenSize])
		if end := nalulen + vt.nalulenSize; len(nalus) >= end {
			vt.Value.AppendRaw(NALUSlice{nalus[vt.nalulenSize:end]})
			nalus = nalus[end:]
		} else {
			util.Printf("WriteAVCC error,len %d,nalulenSize:%d,end:%d", len(nalus), vt.nalulenSize, end)
			break
		}
	}
	vt.Flush()
}

func (vt *H264H265) Flush() {
	// AVCC格式补完
	if vt.Value.AVCC == nil {
		b := []byte{vt.CodecID, 1, 0, 0, 0}
		if vt.Value.IFrame {
			b[0] |= 0x10
		} else {
			b[0] |= 0x20
		}
		// 写入CTS
		util.PutBE(b[2:5], vt.SampleRate.ToMini(vt.Value.PTS-vt.Value.DTS))
		vt.Value.AppendAVCC(b)
		for _, nalu := range vt.Value.Raw {
			vt.Value.AppendAVCC(util.PutBE(make([]byte, 4), SizeOfBuffers(nalu)))
			vt.Value.AppendAVCC(nalu...)
		}
	}
	// 下一帧为I帧，即将覆盖
	if vt.Next().Value.IFrame {
		// 仅存一枚I帧，需要扩环
		if vt.idrCount == 1 {
			if vt.Size < 256 {
				vt.Link(util.NewRing[AVFrame[NALUSlice]](5)) // 扩大缓冲环
			}
		} else {
			vt.idrCount--
		}
	}
	vt.Media.Flush()
}
func (vt *H264H265) ReadRing() *AVRing[NALUSlice] {
	vr := util.Clone(vt.AVRing)
	vr.Ring = vt.IDRing
	return vr
}
func (vt *H264H265) Play(onVideo func(*AVFrame[NALUSlice]) bool) {
	vr := vt.ReadRing()
	for vp := vr.Read(); vt.Stream.Err() == nil; vp = vr.Read() {
		if !onVideo(vp) {
			break
		}
		vr.MoveNext()
	}
}

type UnknowVideo struct {
	Name   string
	Stream IStream
	Know   Video
}

func (vt *UnknowVideo) WriteAnnexB(pts uint32, dts uint32, frame AnnexBFrame) {

}

func (vt *UnknowVideo) WriteAVCC(ts uint32, frame AVCCFrame) {
	if vt.Know == nil {
		if frame.IsSequence() {
			codecID := frame.VideoCodecID()
			if vt.Name == "" {
				vt.Name = strings.ToLower(codec.CodecID[codecID])
			}
			switch codecID {
			case codec.CodecID_H264:
				v := NewH264(vt.Stream)
				vt.Know = v
				v.WriteAVCC(0, frame)
				v.Stream.AddTrack(v.Name, v)
			case codec.CodecID_H265:
				v := NewH265(vt.Stream)
				vt.Know = v
				v.WriteAVCC(0, frame)
				v.Stream.AddTrack(v.Name, v)
			}
		}
	} else {
		vt.Know.WriteAVCC(ts, frame)
	}
}
