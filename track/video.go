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
	GOP         int  //关键帧间隔
	nalulenSize int  //avcc格式中表示nalu长度的字节数，通常为4
	idrCount    int  //缓存中包含的idr数量
	dcChanged   bool //解码器配置是否改变了，一般由于变码率导致
	dtsEst      *DTSEstimator
}

func (vt *Video) GetDecConfSeq() int {
	return vt.DecoderConfiguration.Seq
}
func (vt *Video) Attach() {
	vt.Stream.AddTrack(vt)
}
func (vt *Video) Detach() {
	vt.Stream = nil
	vt.Stream.RemoveTrack(vt)
}
func (vt *Video) GetName() string {
	if vt.Name == "" {
		return vt.CodecID.String()
	}
	return vt.Name
}

func (vt *Video) ComputeGOP() {
	vt.idrCount++
	if vt.IDRing != nil {
		vt.GOP = int(vt.AVRing.RingBuffer.Value.Sequence - vt.IDRing.Value.Sequence)
		if l := vt.AVRing.RingBuffer.Size - vt.GOP - 5; l > 5 {
			vt.AVRing.RingBuffer.Size -= l
			vt.Stream.Debug(Sprintf("resize(%d%s%d)", vt.AVRing.RingBuffer.Size+l, Blink("→"), vt.AVRing.RingBuffer.Size), zap.String("name", vt.Name))
			//缩小缓冲环节省内存
			vt.Unlink(l).Do(func(v AVFrame[NALUSlice]) {
				if v.IFrame {
					vt.idrCount--
				}
				v.Reset()
			})
		}
	}
	vt.IDRing = vt.AVRing.RingBuffer.Ring
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
			vt.AVRing.RingBuffer.Value.AppendRaw(NALUSlice{nalus[vt.nalulenSize:end]})
			nalus = nalus[end:]
		} else {
			vt.Stream.Error("WriteAVCC", zap.Int("len", len(nalus)), zap.Int("naluLenSize", vt.nalulenSize), zap.Int("end", end))
			break
		}
	}
}

func (av *Video) generateTimestamp(ts uint32) {
	av.AVRing.RingBuffer.Value.PTS = ts
	av.AVRing.RingBuffer.Value.DTS = av.dtsEst.Feed(ts)
}

func (vt *Video) Flush() {
	rv := &vt.AVRing.RingBuffer.Value
	// 没有实际媒体数据
	if len(rv.Raw) == 0 {
		rv.Reset()
		return
	}
	// AVCC格式补完
	if len(rv.AVCC) == 0 && (config.Global.EnableAVCC) {
		var b util.Buffer
		if cap(rv.AVCC) > 0 {
			if avcc := rv.AVCC[:1]; len(avcc[0]) == 5 {
				b = util.Buffer(avcc[0])
			}
		}
		if b == nil {
			b = util.Buffer([]byte{0, 1, 0, 0, 0})
		}
		if rv.IFrame {
			b[0] = 0x10 | byte(vt.CodecID)
		} else {
			b[0] = 0x20 | byte(vt.CodecID)
		}
		// 写入CTS
		util.PutBE(b[2:5], (rv.PTS-rv.DTS)/90)
		lengths := b.Malloc(len(rv.Raw) * 4) //每个slice的长度内存复用
		rv.AppendAVCC(b.SubBuf(0, 5))
		for i, nalu := range rv.Raw {
			rv.AppendAVCC(util.PutBE(lengths.SubBuf(i*4, 4), util.SizeOfBuffers(nalu)))
			rv.AppendAVCC(nalu...)
		}
	}
	// 下一帧为I帧，即将覆盖
	if vt.Next().Value.IFrame {
		// 仅存一枚I帧，需要扩环
		if vt.idrCount == 1 {
			if vt.AVRing.RingBuffer.Size < 256 {
				vt.Link(util.NewRing[AVFrame[NALUSlice]](5)) // 扩大缓冲环
			}
		} else {
			vt.idrCount--
		}
	}
	vt.Media.Flush()
}
func (vt *Video) PacketizeRTP(payloads ...[]byte) {
	if vt.AVRing.RingBuffer.Value.IFrame && vt.dcChanged {
		vt.dcChanged = false
		payloads = append(append([][]byte{}, vt.DecoderConfiguration.Raw...), payloads...)
	}
	vt.Media.PacketizeRTP(payloads...)
}
func (vt *Video) ReadRing() *AVRing[NALUSlice] {
	vr := vt.Media.ReadRing()
	vr.Ring = vt.IDRing
	return vr
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
