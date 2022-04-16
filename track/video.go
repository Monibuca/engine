package track

import (
	"bytes"

	. "github.com/logrusorgru/aurora"
	"go.uber.org/zap"
	"m7s.live/engine/v4/codec"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/config"
	"m7s.live/engine/v4/util"
)

type Video struct {
	Media[NALUSlice]
	CodecID     codec.VideoCodecID
	IDRing      *util.Ring[AVFrame[NALUSlice]] `json:"-"` //最近的关键帧位置，首屏渲染
	SPSInfo     codec.SPSInfo
	GOP         int //关键帧间隔
	nalulenSize int //avcc格式中表示nalu长度的字节数，通常为4
	idrCount    int //缓存中包含的idr数量
}

func (t *Video) GetDecConfSeq() int {
	return t.DecoderConfiguration.Seq
}
func (t *Video) Attach() {
	t.Stream.AddTrack(t)
}
func (t *Video) Detach() {
	t.Stream = nil
	t.Stream.RemoveTrack(t)
}
func (t *Video) GetName() string {
	if t.Name == "" {
		return t.CodecID.String()
	}
	return t.Name
}

func (t *Video) ComputeGOP() {
	t.idrCount++
	if t.IDRing != nil {
		t.GOP = int(t.Value.Sequence - t.IDRing.Value.Sequence)
		if l := t.Size - t.GOP - 5; l > 5 {
			t.Size -= l
			t.Stream.Debug(Sprintf("resize(%d%s%d)", t.Size+l, Blink("→"), t.Size), zap.String("name", t.Name))
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

func (vt *Video) writeAnnexBSlice(annexb AnnexBFrame, s *[]NALUSlice) {
	for len(annexb) > 0 {
		before, after, found := bytes.Cut(annexb, codec.NALU_Delimiter1)
		if !found {
			*s = append(*s, NALUSlice{annexb})
			return
		}
		if len(before) > 0 {
			*s = append(*s, NALUSlice{before})
		}
		annexb = after
	}
}

func (vt *Video) WriteAnnexB(pts uint32, dts uint32, frame AnnexBFrame) (s []NALUSlice) {
	// vt.Stream.Tracef("WriteAnnexB:pts %d,dts %d,len %d", pts, dts, len(frame))
	for len(frame) > 0 {
		before, after, found := bytes.Cut(frame, codec.NALU_Delimiter2)
		if !found {
			vt.writeAnnexBSlice(frame, &s)
			if len(vt.Value.Raw) > 0 {
				vt.Value.PTS = pts
				vt.Value.DTS = dts
			}
			return
		}
		if len(before) > 0 {
			vt.writeAnnexBSlice(AnnexBFrame(before), &s)
		}
		frame = after
	}
	return
}
func (vt *Video) WriteAVCC(ts uint32, frame AVCCFrame) {
	vt.Media.WriteAVCC(ts, frame)
	for nalus := frame[5:]; len(nalus) > vt.nalulenSize; {
		nalulen := util.ReadBE[int](nalus[:vt.nalulenSize])
		if end := nalulen + vt.nalulenSize; len(nalus) >= end {
			vt.Value.AppendRaw(NALUSlice{nalus[vt.nalulenSize:end]})
			nalus = nalus[end:]
		} else {
			vt.Stream.Error("WriteAVCC", zap.Int("len", len(nalus)), zap.Int("naluLenSize", vt.nalulenSize), zap.Int("end", end))
			break
		}
	}
}

func (vt *Video) Flush() {
	// 没有实际媒体数据
	if len(vt.Value.Raw) == 0 {
		vt.Value.Reset()
		return
	}
	// AVCC格式补完
	if len(vt.Value.AVCC) == 0 && (config.Global.EnableAVCC || config.Global.EnableFLV) {
		var b util.Buffer
		if cap(vt.Value.AVCC) > 0 {
			if avcc := vt.Value.AVCC[:1]; len(avcc[0]) == 5 {
				b = util.Buffer(avcc[0])
			}
		}
		if b == nil {
			b = util.Buffer([]byte{0, 1, 0, 0, 0})
		}
		if vt.Value.IFrame {
			b[0] = 0x10 | byte(vt.CodecID)
		} else {
			b[0] = 0x20 | byte(vt.CodecID)
		}
		// 写入CTS
		util.PutBE(b[2:5], (vt.Value.PTS-vt.Value.DTS)/90)
		lengths := b.Malloc(len(vt.Value.Raw) * 4) //每个slice的长度内存复用
		vt.Value.AppendAVCC(b.SubBuf(0, 5))
		for i, nalu := range vt.Value.Raw {
			vt.Value.AppendAVCC(util.PutBE(lengths.SubBuf(i*4, 4), util.SizeOfBuffers(nalu)))
			vt.Value.AppendAVCC(nalu...)
		}
	}
	// FLV tag 补完
	if len(vt.Value.FLV) == 0 && config.Global.EnableFLV {
		vt.Value.FillFLV(codec.FLV_TAG_TYPE_VIDEO, vt.Value.AbsTime)
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

func (vt *Video) ReadRing() *AVRing[NALUSlice] {
	vr := vt.Media.ReadRing()
	vr.Ring = vt.IDRing
	return vr
}

type UnknowVideo struct {
	Base
	VideoTrack
}

func (uv *UnknowVideo) GetName() string {
	return uv.Base.GetName()
}

func (uv *UnknowVideo) Flush() {
	uv.VideoTrack.Flush()
}

/*
Access Unit的首个nalu是4字节起始码。
这里举个例子说明，用JM可以生成这样一段码流（不要使用JM8.6，它在这部分与标准不符），这个码流可以见本楼附件：
    SPS          （4字节头）
    PPS          （4字节头）
    SEI          （4字节头）
    I0(slice0)     （4字节头）
    I0(slice1)   （3字节头）
    P1(slice0)     （4字节头）
    P1(slice1)   （3字节头）
    P2(slice0)     （4字节头）
    P2(slice1)   （3字节头）
I0(slice0)是序列第一帧（I帧）的第一个slice，是当前Access Unit的首个nalu，所以是4字节头。而I0(slice1)表示第一帧的第二个slice，所以是3字节头。P1(slice0) 、P1(slice1)同理。

*/
func (vt *UnknowVideo) WriteAnnexB(pts uint32, dts uint32, frame AnnexBFrame) {

}

func (vt *UnknowVideo) WriteAVCC(ts uint32, frame AVCCFrame) {
	if vt.VideoTrack == nil {
		if frame.IsSequence() {
			ts = 0
			codecID := frame.VideoCodecID()
			if vt.Name == "" {
				vt.Name = codecID.String()
			}
			switch codecID {
			case codec.CodecID_H264:
				vt.VideoTrack = NewH264(vt.Stream)
			case codec.CodecID_H265:
				vt.VideoTrack = NewH265(vt.Stream)
			default:
				vt.Stream.Error("video codecID not support: ", zap.Uint8("codeId", uint8(codecID)))
				return
			}
			vt.VideoTrack.WriteAVCC(ts, frame)
		} else {
			vt.Stream.Warn("need sequence frame")
		}
	} else {
		vt.VideoTrack.WriteAVCC(ts, frame)
	}
}
