package track

import (
	"bytes"

	// . "github.com/logrusorgru/aurora"

	"m7s.live/engine/v4/codec"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/util"
)

type Video struct {
	Media
	CodecID     codec.VideoCodecID
	GOP         int  //关键帧间隔
	nalulenSize int  //avcc格式中表示nalu长度的字节数，通常为4
	dcChanged   bool //解码器配置是否改变了，一般由于变码率导致
	dtsEst      *DTSEstimator
	lostFlag    bool // 是否丢帧
	codec.SPSInfo
	ParamaterSets `json:"-"`
	SPS           []byte `json:"-"`
	PPS           []byte `json:"-"`
}

func (v *Video) Attach() {
	if v.Attached.CompareAndSwap(false, true) {
		v.Stream.AddTrack(v)
	}
}

func (v *Video) Detach() {
	if v.Attached.CompareAndSwap(true, false) {
		v.Stream.RemoveTrack(v)
	}
}

func (vt *Video) GetName() string {
	if vt.Name == "" {
		return vt.CodecID.String()
	}
	return vt.Name
}

// PlayFullAnnexB 订阅annex-b格式的流数据，每一个I帧增加sps、pps头
// func (vt *Video) PlayFullAnnexB(ctx context.Context, onMedia func(net.Buffers) error) error {
// 	for vr := vt.ReadRing(); ctx.Err() == nil; vr.MoveNext() {
// 		vp := vr.Read(ctx)
// 		var data net.Buffers
// 		if vp.IFrame {
// 			data = vt.GetAnnexB()
// 		}
// 		data = append(data, codec.NALU_Delimiter2)
// 		for slice := vp.AUList.Head; slice != nil; slice = slice.Next {
// 			data = append(data, slice.ToBuffers()...)
// 			if slice.Next != nil {
// 				data = append(data, codec.NALU_Delimiter1)
// 			}
// 		}

//			if err := onMedia(data); err != nil {
//				// TODO: log err
//				return err
//			}
//		}
//		return ctx.Err()
//	}
func (vt *Video) computeGOP() {
	if vt.HistoryRing == nil && vt.IDRing != nil {
		vt.GOP = int(vt.Value.Sequence - vt.IDRing.Value.Sequence)
		vt.narrow(vt.GOP)
	}
	vt.IDRing = vt.Ring
	// var n int
	// for i := 0; i < len(vt.BytesPool); i++ {
	// 	n += vt.BytesPool[i].Length
	// }
	// println(n)
}

func (vt *Video) writeAnnexBSlice(annexb AnnexBFrame) {
	for found, after := true, annexb; len(annexb) > 0 && found; annexb = after {
		annexb, after, found = bytes.Cut(annexb, codec.NALU_Delimiter1)
		vt.WriteSliceBytes(annexb)
	}
}

func (vt *Video) WriteAnnexB(pts uint32, dts uint32, frame AnnexBFrame) {
	if dts == 0 {
		vt.generateTimestamp(pts)
	} else {
		vt.Value.PTS = pts
		vt.Value.DTS = dts
	}
	vt.Value.BytesIn += len(frame)
	for found, after := true, frame; len(frame) > 0 && found; frame = after {
		frame, after, found = bytes.Cut(frame, codec.NALU_Delimiter2)
		vt.writeAnnexBSlice(frame)
	}
	vt.Flush()
}

func (vt *Video) WriteAVCC(ts uint32, frame util.BLL) {
	r := frame.NewReader()
	b, err := r.ReadByte()
	if err != nil {
		return
	}
	b = b >> 4
	vt.Value.IFrame = b == 1 || b == 4
	r.ReadByte() //sequence frame flag
	vt.Value.WriteAVCC(ts, frame)
	cts, err := r.ReadBE(3)
	if err != nil {
		return
	}
	vt.Value.PTS = (ts + cts) * 90
	for nalulen, err := r.ReadBE(vt.nalulenSize); err == nil; nalulen, err = r.ReadBE(vt.nalulenSize) {
		vt.AppendAuBytes(r.ReadN(int(nalulen))...)
	}
	vt.Flush()
}

func (vt *Video) WriteSliceByte(b ...byte) {
	vt.WriteSliceBytes(b)
}

// 在I帧前面插入sps pps webrtc需要
func (av *Video) insertDCRtp() {
	seq := av.Value.RTP[0].SequenceNumber
	l1, l2 := len(av.ParamaterSets), len(av.Value.RTP)
	afterLen := l1 + l2
	if cap(av.Value.RTP) < afterLen {
		rtps := make([]*RTPFrame, l1, afterLen)
		av.Value.RTP = append(rtps, av.Value.RTP...)
	} else {
		av.Value.RTP = av.Value.RTP[:afterLen]
		copy(av.Value.RTP[l1:], av.Value.RTP[:l2])
	}
	for i, nalu := range av.ParamaterSets {
		packet := &RTPFrame{}
		packet.Version = 2
		packet.PayloadType = av.PayloadType
		packet.Payload = nalu
		packet.SSRC = av.SSRC
		packet.SequenceNumber = seq
		packet.Timestamp = av.Value.PTS
		packet.Marker = false
		seq++
		av.rtpSequence++
		av.Value.RTP[i] = packet
	}
	for i := l1; i < afterLen; i++ {
		av.Value.RTP[i].SequenceNumber = seq
		seq++
	}
}

func (av *Video) generateTimestamp(ts uint32) {
	av.Value.PTS = ts
	av.Value.DTS = av.dtsEst.Feed(ts)
}

func (vt *Video) SetLostFlag() {
	vt.lostFlag = true
}
func (vt *Video) CompleteAVCC(rv *AVFrame) {
	mem := vt.BytesPool.Get(5)
	b := mem.Value
	if rv.IFrame {
		b[0] = 0x10 | byte(vt.CodecID)
	} else {
		b[0] = 0x20 | byte(vt.CodecID)
	}
	b[1] = 1
	// println(rv.PTS < rv.DTS, "\t", rv.PTS, "\t", rv.DTS, "\t", rv.PTS-rv.DTS)
	// 写入CTS
	util.PutBE(b[2:5], (rv.PTS-rv.DTS)/90)
	rv.AVCC.Push(mem)
	vt.Value.AUList.Range(func(au *util.BLL) bool {
		mem = vt.BytesPool.Get(4)
		util.PutBE(mem.Value, uint32(au.ByteLength))
		vt.Value.AVCC.Push(mem)
		au.Range(func(slice util.BLI) bool {
			vt.Value.AVCC.Push(vt.BytesPool.GetShell(slice))
			return true
		})
		return true
	})
}

func (vt *Video) Flush() {
	rv := &vt.Value
	if rv.AUList.Length == 0 {
		rv.Reset()
		return
	}
	if rv.IFrame {
		vt.computeGOP()
		vt.Stream.SetIDR(vt)
	}
	if !vt.Attached.Load() && vt.IDRing != nil && vt.SequenceHeadSeq > 0 {
		defer vt.Attach()
	}

	if vt.lostFlag {
		if rv.IFrame {
			vt.lostFlag = false
		} else {
			rv.Reset()
			return
		}
	}

	vt.Media.Flush()
	vt.dcChanged = false
}

func (vt *Video) updateSequeceHead() {
	vt.dcChanged = true
	vt.SequenceHeadSeq++
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
