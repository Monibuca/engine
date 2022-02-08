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
		if len(vt.DecoderConfiguration.Raw) > 0 {
			vt.DecoderConfiguration.Raw = vt.DecoderConfiguration.Raw[:0]
		}
		vt.DecoderConfiguration.Raw = append(vt.DecoderConfiguration.Raw, slice[0])
	case codec.NALU_PPS:
		vt.DecoderConfiguration.Raw = append(vt.DecoderConfiguration.Raw, slice[0])
		vt.SPSInfo, _ = codec.ParseSPS(slice[0])
		if len(vt.DecoderConfiguration.Raw) > 0 {
			vt.DecoderConfiguration.Raw = vt.DecoderConfiguration.Raw[:0]
		}
		lenSPS := len(vt.DecoderConfiguration.Raw[0])
		lenPPS := len(vt.DecoderConfiguration.Raw[1])
		if len(vt.DecoderConfiguration.AVCC) > 0 {
			vt.DecoderConfiguration.AVCC = vt.DecoderConfiguration.AVCC[:0]
		}
		if lenSPS > 3 {
			vt.DecoderConfiguration.AVCC = append(vt.DecoderConfiguration.AVCC, codec.RTMP_AVC_HEAD[:6], vt.DecoderConfiguration.Raw[0][1:4])
		} else {
			vt.DecoderConfiguration.AVCC = append(vt.DecoderConfiguration.AVCC, codec.RTMP_AVC_HEAD)
		}
		tmp := []byte{0xE1, 0, 0, 0x01, 0, 0}
		vt.DecoderConfiguration.AVCC = append(vt.DecoderConfiguration.AVCC, tmp[:1], util.PutBE(tmp[1:3], lenSPS), vt.DecoderConfiguration.Raw[0], tmp[3:4], util.PutBE(tmp[3:6], lenPPS), vt.DecoderConfiguration.Raw[1])
		vt.DecoderConfiguration.FLV = codec.VideoAVCC2FLV(net.Buffers(vt.DecoderConfiguration.AVCC), 0)
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
		if len(vt.DecoderConfiguration.AVCC) > 0 {
			vt.DecoderConfiguration.AVCC = vt.DecoderConfiguration.AVCC[:0]
		}
		vt.DecoderConfiguration.AVCC = append(vt.DecoderConfiguration.AVCC, frame)
		var info codec.AVCDecoderConfigurationRecord
		if _, err := info.Unmarshal(frame[5:]); err == nil {
			vt.SPSInfo, _ = codec.ParseSPS(info.SequenceParameterSetNALUnit)
			vt.nalulenSize = int(info.LengthSizeMinusOne&3 + 1)
			vt.DecoderConfiguration.Raw = NALUSlice{info.SequenceParameterSetNALUnit, info.PictureParameterSetNALUnit}
		}
		vt.DecoderConfiguration.FLV = codec.VideoAVCC2FLV(net.Buffers(vt.DecoderConfiguration.AVCC), 0)
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
