package track

import (
	"net"
	"time"

	"github.com/Monibuca/engine/v4/codec"
	. "github.com/Monibuca/engine/v4/common"
	"github.com/Monibuca/engine/v4/util"
)

type H264 Video

func NewH264(stream IStream) (vt *H264) {
	vt = &H264{}
	vt.Name = "h264"
	vt.CodecID = codec.CodecID_H264
	vt.SampleRate = 90000
	vt.Stream = stream
	vt.Init(stream, 256)
	vt.Poll = time.Millisecond * 20
	return
}
func (vt *H264) WriteAnnexB(pts uint32, dts uint32, frame AnnexBFrame) {
	(*Video)(vt).WriteAnnexB(pts, dts, frame)
	vt.Flush()
}
func (vt *H264) WriteSlice(slice NALUSlice) {
	switch slice.H264Type() {
	case codec.NALU_SPS:
		vt.DecoderConfiguration.Reset()
		vt.DecoderConfiguration.AppendRaw(slice)
	case codec.NALU_PPS:
		vt.DecoderConfiguration.AppendRaw(slice)
		vt.SPSInfo, _ = codec.ParseSPS(slice[0])
		lenSPS := util.SizeOfBuffers(net.Buffers(vt.DecoderConfiguration.Raw[0]))
		lenPPS := util.SizeOfBuffers(net.Buffers(vt.DecoderConfiguration.Raw[1]))
		if lenSPS > 3 {
			vt.DecoderConfiguration.AppendAVCC(codec.RTMP_AVC_HEAD[:6], vt.DecoderConfiguration.Raw[0][0][1:4])
		} else {
			vt.DecoderConfiguration.AppendAVCC(codec.RTMP_AVC_HEAD)
		}
		tmp := []byte{0xE1, 0, 0, 0x01, 0, 0}
		vt.DecoderConfiguration.AppendAVCC(tmp[:1], util.PutBE(tmp[1:3], lenSPS), vt.DecoderConfiguration.Raw[0][0], tmp[3:4], util.PutBE(tmp[3:6], lenPPS), vt.DecoderConfiguration.Raw[1][0])
	case codec.NALU_IDR_Picture:
		vt.Value.IFrame = true
		fallthrough
	case codec.NALU_Non_IDR_Picture,
		codec.NALU_SEI:
		vt.Media.WriteSlice(slice)
	}
}

func (vt *H264) WriteAVCC(ts uint32, frame AVCCFrame) {
	if frame.IsSequence() {
		vt.DecoderConfiguration.Reset()
		vt.DecoderConfiguration.SeqInTrack = vt.Value.SeqInTrack
		vt.DecoderConfiguration.AppendAVCC(frame)
		var info codec.AVCDecoderConfigurationRecord
		if _, err := info.Unmarshal(frame[5:]); err == nil {
			vt.SPSInfo, _ = codec.ParseSPS(info.SequenceParameterSetNALUnit)
			vt.nalulenSize = int(info.LengthSizeMinusOne&3 + 1)
			vt.DecoderConfiguration.AppendRaw(NALUSlice{info.SequenceParameterSetNALUnit}, NALUSlice{info.PictureParameterSetNALUnit})
		}
		vt.DecoderConfiguration.FillFLV(codec.FLV_TAG_TYPE_VIDEO, 0)
	} else {
		(*Video)(vt).WriteAVCC(ts, frame)
		vt.Value.IFrame = frame.IsIDR()
		vt.Flush()
	}
}

func (vt *H264) Flush() {
	if vt.Value.IFrame {
		if vt.IDRing == nil {
			defer vt.Stream.AddTrack(vt)
		}
		(*Video)(vt).ComputeGOP()
	}
	// RTP格式补完
	if vt.Value.RTP == nil {

	}
	(*Video)(vt).Flush()
}
