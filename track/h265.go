package track

import (
	"net"
	"time"

	"github.com/Monibuca/engine/v4/codec"
	. "github.com/Monibuca/engine/v4/common"
)

type H265 Video

func NewH265(stream IStream) (vt *H265) {
	vt = &H265{}
	vt.Name = "h265"
	vt.CodecID = codec.CodecID_H265
	vt.SampleRate = 90000
	vt.Stream = stream
	vt.Init(stream, 256)
	vt.Poll = time.Millisecond * 20
	return
}
func (vt *H265) WriteAnnexB(pts uint32, dts uint32, frame AnnexBFrame) {
	(*Video)(vt).WriteAnnexB(pts, dts, frame)
	vt.Flush()
}
func (vt *H265) WriteSlice(slice NALUSlice) {
	switch slice.H265Type() {
	case codec.NAL_UNIT_VPS:
		if len(vt.DecoderConfiguration.Raw) > 0 {
			vt.DecoderConfiguration.Raw = vt.DecoderConfiguration.Raw[:0]
		}
		vt.DecoderConfiguration.Raw = append(vt.DecoderConfiguration.Raw, slice[0])
	case codec.NAL_UNIT_SPS:
		vt.DecoderConfiguration.Raw = append(vt.DecoderConfiguration.Raw, slice[0])
		vt.SPSInfo, _ = codec.ParseHevcSPS(slice[0])
	case codec.NAL_UNIT_PPS:
		vt.DecoderConfiguration.Raw = append(vt.DecoderConfiguration.Raw, slice[0])
		extraData, err := codec.BuildH265SeqHeaderFromVpsSpsPps(vt.DecoderConfiguration.Raw[0], vt.DecoderConfiguration.Raw[1], vt.DecoderConfiguration.Raw[2])
		if err == nil {
			if len(vt.DecoderConfiguration.AVCC) > 0 {
				vt.DecoderConfiguration.AVCC = vt.DecoderConfiguration.AVCC[:0]
			}
			vt.DecoderConfiguration.AVCC = append(vt.DecoderConfiguration.AVCC, extraData)
		}
		vt.DecoderConfiguration.FLV = codec.VideoAVCC2FLV(net.Buffers(vt.DecoderConfiguration.AVCC), 0)
	case
		codec.NAL_UNIT_CODED_SLICE_BLA,
		codec.NAL_UNIT_CODED_SLICE_BLANT,
		codec.NAL_UNIT_CODED_SLICE_BLA_N_LP,
		codec.NAL_UNIT_CODED_SLICE_IDR,
		codec.NAL_UNIT_CODED_SLICE_IDR_N_LP,
		codec.NAL_UNIT_CODED_SLICE_CRA:
		vt.Value.IFrame = true
		fallthrough
	case 0, 1, 2, 3, 4, 5, 6, 7, 9:
		vt.Media.WriteSlice(slice)
	}
}
func (vt *H265) WriteAVCC(ts uint32, frame AVCCFrame) {
	if frame.IsSequence() {
		if len(vt.DecoderConfiguration.AVCC) > 0 {
			vt.DecoderConfiguration.AVCC = vt.DecoderConfiguration.AVCC[:0]
		}
		vt.DecoderConfiguration.AVCC = append(vt.DecoderConfiguration.AVCC, frame)
		if vps, sps, pps, err := codec.ParseVpsSpsPpsFromSeqHeaderWithoutMalloc(frame); err == nil {
			vt.SPSInfo, _ = codec.ParseHevcSPS(frame)
			vt.nalulenSize = int(frame[26]) & 0x03
			vt.DecoderConfiguration.Raw = NALUSlice{vps, sps, pps}
		}
		vt.DecoderConfiguration.FLV = codec.VideoAVCC2FLV(net.Buffers(vt.DecoderConfiguration.AVCC), 0)
	} else {
		(*Video)(vt).WriteAVCC(ts, frame)
		vt.Value.IFrame = frame.IsIDR()
		vt.Flush()
	}
}

func (vt *H265) Flush() {
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
