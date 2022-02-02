package track

import (
	"time"

	"github.com/Monibuca/engine/v4/codec"
	. "github.com/Monibuca/engine/v4/common"
)

type H265 struct {
	H264H265
}

func NewH265(stream IStream) (vt *H265) {
	vt = &H265{}
	vt.CodecID = codec.CodecID_H265
	vt.SampleRate = 90000
	vt.Stream = stream
	vt.Init(stream, 256)
	vt.Poll = time.Millisecond * 20
	return
}
func (vt *H265) WriteSlice(slice NALUSlice) {
	switch H265Slice(slice).Type() {
	case codec.NAL_UNIT_VPS:
		vt.DecoderConfiguration.Reset()
		vt.DecoderConfiguration.AppendRaw(slice)
	case codec.NAL_UNIT_SPS:
		vt.DecoderConfiguration.AppendRaw(slice)
		vt.SPSInfo, _ = codec.ParseHevcSPS(slice[0])
	case codec.NAL_UNIT_PPS:
		vt.DecoderConfiguration.AppendRaw(slice)
		extraData, err := codec.BuildH265SeqHeaderFromVpsSpsPps(vt.DecoderConfiguration.Raw[0][0], vt.DecoderConfiguration.Raw[1][0], vt.DecoderConfiguration.Raw[2][0])
		if err == nil {
			vt.DecoderConfiguration.AppendAVCC(extraData)
		}
	case 0, 1, 2, 3, 4, 5, 6, 7, 9,
		codec.NAL_UNIT_CODED_SLICE_BLA,
		codec.NAL_UNIT_CODED_SLICE_BLANT,
		codec.NAL_UNIT_CODED_SLICE_BLA_N_LP,
		codec.NAL_UNIT_CODED_SLICE_IDR,
		codec.NAL_UNIT_CODED_SLICE_IDR_N_LP,
		codec.NAL_UNIT_CODED_SLICE_CRA:
		vt.Media.WriteSlice(slice)
	}
}
func (vt *H265) WriteAVCC(ts uint32, frame AVCCFrame) {
	if frame.IsSequence() {
		vt.DecoderConfiguration.Reset()
		vt.DecoderConfiguration.SeqInTrack = vt.Value.SeqInTrack
		vt.DecoderConfiguration.AppendAVCC(frame)
		if vps, sps, pps, err := codec.ParseVpsSpsPpsFromSeqHeaderWithoutMalloc(frame); err == nil {
			vt.SPSInfo, _ = codec.ParseHevcSPS(frame)
			vt.nalulenSize = int(frame[26]) & 0x03
			vt.DecoderConfiguration.AppendRaw(NALUSlice{vps}, NALUSlice{sps}, NALUSlice{pps})
		}
	} else {
		vt.H264H265.WriteAVCC(ts, frame)
	}
}

func (vt *H265) Flush() {
	if H265NALU(vt.Value.Raw).IFrame() {
		vt.Value.IFrame = true
		if vt.IDRing == nil {
			defer vt.Stream.AddTrack(vt.Name, vt)
		}
		vt.ComputeGOP()
	}
	// RTP格式补完
	if vt.Value.RTP == nil {

	}
	vt.H264H265.Flush()
}
