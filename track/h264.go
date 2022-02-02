package track

import (
	"time"

	"github.com/Monibuca/engine/v4/codec"
	. "github.com/Monibuca/engine/v4/common"
	"github.com/Monibuca/engine/v4/util"
)

type H264 struct {
	H264H265
}

func NewH264(stream IStream) (vt *H264) {
	vt = &H264{}
	vt.CodecID = codec.CodecID_H264
	vt.SampleRate = 90000
	vt.Stream = stream
	vt.Init(stream, 256)
	vt.Poll = time.Millisecond * 20
	return
}

func (vt *H264) WriteSlice(slice NALUSlice) {
	switch H264Slice(slice).Type() {
	case codec.NALU_SPS:
		vt.DecoderConfiguration.Reset()
		vt.DecoderConfiguration.AppendRaw(slice)
	case codec.NALU_PPS:
		vt.DecoderConfiguration.AppendRaw(slice)
		vt.SPSInfo, _ = codec.ParseSPS(slice[0])
		lenSPS := SizeOfBuffers(vt.DecoderConfiguration.Raw[0])
		lenPPS := SizeOfBuffers(vt.DecoderConfiguration.Raw[1])
		if lenSPS > 3 {
			vt.DecoderConfiguration.AppendAVCC(codec.RTMP_AVC_HEAD[:6], vt.DecoderConfiguration.Raw[0][0][1:4])
		} else {
			vt.DecoderConfiguration.AppendAVCC(codec.RTMP_AVC_HEAD)
		}
		tmp := []byte{0xE1, 0, 0, 0x01, 0, 0}
		vt.DecoderConfiguration.AppendAVCC(tmp[:1], util.PutBE(tmp[1:3], lenSPS), vt.DecoderConfiguration.Raw[0][0], tmp[3:4], util.PutBE(tmp[3:6], lenPPS), vt.DecoderConfiguration.Raw[1][0])
	case codec.NALU_IDR_Picture:
	case codec.NALU_Non_IDR_Picture:
	case codec.NALU_SEI:
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
	} else {
		vt.H264H265.WriteAVCC(ts, frame)
	}
}

func (vt *H264) Flush() {
	if H264NALU(vt.Value.Raw).IFrame() {
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
