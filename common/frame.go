package common

import (
	"net"
	"time"

	"github.com/Monibuca/engine/v4/codec"
	"github.com/pion/rtp"
)

type NALUSlice net.Buffers

// type H264Slice NALUSlice
// type H265Slice NALUSlice

// type H264NALU []H264Slice
// type H265NALU []H265Slice

type AudioSlice []byte

// type AACSlice AudioSlice
// type G711Slice AudioSlice

// 裸数据片段
type RawSlice interface {
	~[][]byte | ~[]byte
}

// func (nalu *H264NALU) Append(slice ...NALUSlice) {
// 	*nalu = append(*nalu, slice...)
// }
func (nalu NALUSlice) H264Type() byte {
	return nalu[0][0] & 0x1F
}
func (nalu NALUSlice) H265Type() byte {
	return nalu[0][0] & 0x7E >> 1
}

// func (nalu *H265NALU) Append(slice ...NALUSlice) {
// 	*nalu = append(*nalu, slice...)
// }
// func (nalu H265NALU) IFrame() bool {
// 	switch H265Slice(nalu[0]).Type() {
// 	case codec.NAL_UNIT_CODED_SLICE_BLA,
// 		codec.NAL_UNIT_CODED_SLICE_BLANT,
// 		codec.NAL_UNIT_CODED_SLICE_BLA_N_LP,
// 		codec.NAL_UNIT_CODED_SLICE_IDR,
// 		codec.NAL_UNIT_CODED_SLICE_IDR_N_LP,
// 		codec.NAL_UNIT_CODED_SLICE_CRA:
// 		return true
// 	}
// 	return false
// }

type AVCCFrame []byte   // 一帧AVCC格式的数据
type AnnexBFrame []byte // 一帧AnnexB格式数据
type BaseFrame struct {
	DeltaTime   uint32 // 相对上一帧时间戳，毫秒
	SeqInStream uint32 //在一个流中的总序号
	SeqInTrack  uint32 //在一个Track中的序号
	BytesIn     int    // 输入字节数用于计算BPS
}

type DataFrame[T any] struct {
	Timestamp time.Time // 写入时间
	BaseFrame
	Value T
}
type AVFrame[T RawSlice] struct {
	BaseFrame
	IFrame     bool
	PTS        uint32
	DTS        uint32
	FLV        net.Buffers // 打包好的FLV Tag
	AVCC       net.Buffers // 打包好的AVCC格式
	RTP        net.Buffers // 打包好的RTP格式
	RTPPackets []rtp.Packet
	Raw        []T //裸数据
	canRead    bool
}

func (av *AVFrame[T]) AppendRaw(raw ...T) {
	av.Raw = append(av.Raw, raw...)
}
func (av *AVFrame[T]) FillFLV(t byte, ts uint32) {
	av.FLV = codec.VideoAVCC2FLV(av.AVCC, ts)
	av.FLV[0][0] = t
}
func (av *AVFrame[T]) AppendAVCC(avcc ...[]byte) {
	av.AVCC = append(av.AVCC, avcc...)
}
func (av *AVFrame[T]) AppendRTP(rtp []byte) {
	av.RTP = append(av.RTP, rtp)
}
func (av *AVFrame[T]) AppendRTPPackets(rtp rtp.Packet) {
	av.RTPPackets = append(av.RTPPackets, rtp)
}

func (av *AVFrame[T]) Reset() {
	av.FLV = nil
	av.AVCC = nil
	av.RTP = nil
	av.RTPPackets = nil
	av.Raw = nil
}

func (avcc AVCCFrame) IsIDR() bool {
	return avcc[0]>>4 == 1
}
func (avcc AVCCFrame) IsSequence() bool {
	return avcc[1] == 0
}
func (avcc AVCCFrame) CTS() uint32 {
	return uint32(avcc[2])<<24 | uint32(avcc[3])<<8 | uint32(avcc[4])
}
func (avcc AVCCFrame) VideoCodecID() byte {
	return avcc[0] & 0x0F
}
func (avcc AVCCFrame) AudioCodecID() byte {
	return avcc[0] >> 4
}

// func (annexb AnnexBFrame) ToSlices() (ret []NALUSlice) {
// 	for len(annexb) > 0 {
// 		before, after, found := bytes.Cut(annexb, codec.NALU_Delimiter1)
// 		if !found {
// 			return append(ret, NALUSlice{annexb})
// 		}
// 		if len(before) > 0 {
// 			ret = append(ret, NALUSlice{before})
// 		}
// 		annexb = after
// 	}
// 	return
// }
// func (annexb AnnexBFrame) ToNALUs() (ret [][]NALUSlice) {
// 	for len(annexb) > 0 {
// 		before, after, found := bytes.Cut(annexb, codec.NALU_Delimiter1)
// 		if !found {
// 			return append(ret, annexb.ToSlices())
// 		}
// 		if len(before) > 0 {
// 			ret = append(ret, AnnexBFrame(before).ToSlices())
// 		}
// 		annexb = after
// 	}
// 	return
// }
type DecoderConfiguration[T RawSlice] struct {
	AVCC T
	Raw  T
	FLV  net.Buffers
}
