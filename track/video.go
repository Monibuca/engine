package track

import (
	"io"
	"time"

	"github.com/pion/rtp"
	"go.uber.org/zap"
	"m7s.live/engine/v4/codec"
	"m7s.live/engine/v4/common"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/util"
)

type Video struct {
	Media
	CodecID     codec.VideoCodecID
	GOP         int  //关键帧间隔
	nalulenSize int  //avcc格式中表示nalu长度的字节数，通常为4
	dcChanged   bool //解码器配置是否改变了，一般由于变码率导致
	dtsEst      *util.DTSEstimator
	lostFlag    bool // 是否丢帧
	codec.SPSInfo
	ParamaterSets  `json:"-" yaml:"-"`
	SPS            []byte `json:"-" yaml:"-"`
	PPS            []byte `json:"-" yaml:"-"`
	iframeReceived bool
}

func (v *Video) Attach() {
	v.Info("attach video track", zap.Uint("width", v.Width), zap.Uint("height", v.Height))
	if err := v.Publisher.GetStream().AddTrack(v).Await(); err != nil {
		v.Error("attach video track failed", zap.Error(err))
	} else {
		v.Info("video track attached", zap.Uint("width", v.Width), zap.Uint("height", v.Height))
	}
}

func (v *Video) Detach() {
	v.Publisher.GetStream().RemoveTrack(v)
}

func (vt *Video) GetName() string {
	if vt.Name == "" {
		return vt.CodecID.String()
	}
	return vt.Name
}

func (vt *Video) GetCodec() codec.VideoCodecID {
	return vt.CodecID
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
	if vt.IDRing != nil {
		vt.GOP = int(vt.Value.Sequence - vt.IDRing.Value.Sequence)
		if vt.HistoryRing == nil {
			vt.narrow(vt.GOP)
		}
	}
	vt.AddIDR()
	// var n int
	// for i := 0; i < len(vt.BytesPool); i++ {
	// 	n += vt.BytesPool[i].Length
	// }
	// println(n)
}

func (vt *Video) writeAnnexBSlice(nalu []byte) {
	common.SplitAnnexB(nalu, vt.WriteSliceBytes, codec.NALU_Delimiter1)
}

func (vt *Video) WriteNalu(pts uint32, dts uint32, nalu []byte) {
	if dts == 0 {
		vt.generateTimestamp(pts)
	} else {
		vt.Value.PTS = time.Duration(pts)
		vt.Value.DTS = time.Duration(dts)
	}
	vt.Value.BytesIn += len(nalu)
	vt.WriteSliceBytes(nalu)
	vt.Flush()
}

func (vt *Video) WriteAnnexB(pts uint32, dts uint32, frame []byte) {
	if dts == 0 {
		vt.generateTimestamp(pts)
	} else {
		vt.Value.PTS = time.Duration(pts)
		vt.Value.DTS = time.Duration(dts)
	}
	vt.Value.BytesIn += len(frame)
	common.SplitAnnexB(frame, vt.writeAnnexBSlice, codec.NALU_Delimiter2)
	if vt.Value.AUList.ByteLength > 0 {
		vt.Flush()
	}
}

func (vt *Video) writeAVCCFrame(ts uint32, r *util.BLLReader, frame *util.BLL) (err error) {
	var cts uint32
	cts, err = r.ReadBE(3)
	if err != nil {
		return err
	}
	vt.Value.PTS = time.Duration(ts+cts) * 90
	vt.Value.DTS = time.Duration(ts) * 90
	var nalulen uint32
	for nalulen, err = r.ReadBE(vt.nalulenSize); err == nil; nalulen, err = r.ReadBE(vt.nalulenSize) {
		if remain := frame.ByteLength - r.GetOffset(); remain < int(nalulen) {
			vt.Error("read nalu length error", zap.Int("nalulen", int(nalulen)), zap.Int("remain", remain))
			frame.Recycle()
			vt.Value.Reset()
			return
		}
		vt.AppendAuBytes(r.ReadN(int(nalulen))...)
	}
	return nil
}

func (vt *Video) WriteAVCC(ts uint32, frame *util.BLL) (err error) {
	if l := frame.ByteLength; l < 6 {
		vt.Error("AVCC data too short", zap.Int("len", l))
		return io.ErrShortWrite
	}
	// bbb := util.Buffer(frame.ToBytes()[5:])
	r := frame.NewReader()
	b, _ := r.ReadByte()
	isExtHeader := (b >> 4) & 0b1000
	frameType := (b >> 4) & 0b0111
	vt.Value.IFrame = frameType == 1 || frameType == 4
	packetType := b & 0b1111
	if isExtHeader != 0 {
		r.ReadBE(4) // fourcc
		switch packetType {
		case codec.PacketTypeSequenceStart:
			err = vt.SpesificTrack.WriteSequenceHead(frame.ToBytes())
			frame.Recycle()
			return
		case codec.PacketTypeCodedFrames:
			err = vt.SpesificTrack.writeAVCCFrame(ts, r, frame)
		case codec.PacketTypeCodedFramesX:
		}
	} else {
		b, _ = r.ReadByte() //sequence frame flag
		if b == 0 {
			err = vt.SpesificTrack.WriteSequenceHead(frame.ToBytes())
			frame.Recycle()
			return
		}
		err = vt.SpesificTrack.writeAVCCFrame(ts, r, frame)
	}
	if err == nil {
		vt.Value.WriteAVCC(ts, frame)
		vt.Flush()
	}
	return
}

func (vt *Video) WriteSliceByte(b ...byte) {
	// fmt.Println("write slice byte", b)
	vt.WriteSliceBytes(b)
}

// 在I帧前面插入sps pps webrtc需要
func (vt *Video) insertDCRtp() {
	head := vt.Value.RTP.Next
	for _, nalu := range vt.ParamaterSets {
		var packet rtp.Packet
		packet.Version = 2
		packet.PayloadType = vt.PayloadType
		packet.Payload = nalu
		packet.SSRC = vt.SSRC
		packet.Timestamp = uint32(vt.Value.PTS)
		packet.Marker = false
		head.InsertBeforeValue(RTPFrame{Packet: &packet})
	}
}

func (vt *Video) generateTimestamp(ts uint32) {
	if vt.State == TrackStateOffline {
		vt.dtsEst = util.NewDTSEstimator()
	}
	vt.Value.PTS = time.Duration(ts)
	vt.Value.DTS = time.Duration(vt.dtsEst.Feed(ts))
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
	// vt.Info("cts", zap.Uint32("cts", uint32((rv.PTS-rv.DTS)/90)))
	// 写入CTS
	util.PutBE(b[2:5], (rv.PTS-rv.DTS)/90)
	rv.AVCC.Push(mem)
	// if rv.AVCC.ByteLength != 5 {
	// 	panic("error")
	// }
	// var tmp = 0
	rv.AUList.Range(func(au *util.BLL) bool {
		mem = vt.BytesPool.Get(4)
		// println(au.ByteLength)
		util.PutBE(mem.Value, uint32(au.ByteLength))
		rv.AVCC.Push(mem)
		au.Range(func(slice util.Buffer) bool {
			rv.AVCC.Push(vt.BytesPool.GetShell(slice))
			return true
		})
		// tmp += 4 + au.ByteLength
		// if rv.AVCC.ByteLength != 5+tmp {
		// 	panic("error")
		// }
		return true
	})
}

func (vt *Video) Flush() {
	rv := vt.Value
	if rv.IFrame {
		vt.computeGOP()
		if audioTrack := vt.Publisher.GetAudioTrack(); audioTrack != nil {
			audioTrack.Narrow()
		}
	}

	if !vt.iframeReceived {
		if vt.IDRing != nil && vt.SequenceHeadSeq > 0 {
			defer vt.Attach()
			vt.iframeReceived = true
		} else {
			rv.Reset()
			return
		}
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

func (vt *Video) WriteSequenceHead(sh []byte) {
	vt.Media.WriteSequenceHead(sh)
	vt.dcChanged = true
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
