package codec

import (
	"bytes"
	"errors"
	"io"

	"m7s.live/engine/v4/util"
	"m7s.live/engine/v4/util/bits/pio"
)

// Start Code + NAL Unit -> NALU Header + NALU Body
// RTP Packet -> NALU Header + NALU Body

// NALU Body -> Slice Header + Slice data
// Slice data -> flags + Macroblock layer1 + Macroblock layer2 + ...
// Macroblock layer1 -> mb_type + PCM Data
// Macroblock layer2 -> mb_type + Sub_mb_pred or mb_pred + Residual Data
// Residual Data ->
type H264NALUType byte

func (b H264NALUType) Or(b2 byte) byte {
	return byte(b) | b2
}

func (b H264NALUType) Offset() int {
	switch b {
	case NALU_STAPA:
		return 1
	case NALU_STAPB:
		return 3
	case NALU_FUA:
		return 2
	case NALU_FUB:
		return 4
	}
	return 0
}

func (b H264NALUType) Byte() byte {
	return byte(b)
}

func (H264NALUType) Parse(b byte) H264NALUType {
	return H264NALUType(b & 0x1F)
}

func (H264NALUType) ParseBytes(bs []byte) H264NALUType {
	return H264NALUType(bs[0] & 0x1F)
}

const (
	// NALU Type
	NALU_Unspecified           H264NALUType = iota
	NALU_Non_IDR_Picture                    // 1
	NALU_Data_Partition_A                   // 2
	NALU_Data_Partition_B                   // 3
	NALU_Data_Partition_C                   // 4
	NALU_IDR_Picture                        // 5
	NALU_SEI                                // 6
	NALU_SPS                                // 7
	NALU_PPS                                // 8
	NALU_Access_Unit_Delimiter              // 9
	NALU_Sequence_End                       // 10
	NALU_Stream_End                         // 11
	NALU_Filler_Data                        // 12
	NALU_SPS_Extension                      // 13
	NALU_Prefix                             // 14
	NALU_SPS_Subset                         // 15
	NALU_DPS                                // 16
	NALU_Reserved1                          // 17
	NALU_Reserved2                          // 18
	NALU_Not_Auxiliary_Coded                // 19
	NALU_Coded_Slice_Extension              // 20
	NALU_Reserved3                          // 21
	NALU_Reserved4                          // 22
	NALU_Reserved5                          // 23
	NALU_STAPA                              // 24
	NALU_STAPB
	NALU_MTAP16
	NALU_MTAP24
	NALU_FUA // 28
	NALU_FUB
	// 24 - 31 NALU_NotReserved

)

var (
	NALU_AUD_BYTE   = []byte{0x00, 0x00, 0x00, 0x01, 0x09, 0xF0}
	NALU_Delimiter1 = []byte{0x00, 0x00, 0x01}
	NALU_Delimiter2 = []byte{0x00, 0x00, 0x00, 0x01}
	// 0x17 keyframe  7:AVC
	// 0x00 AVC sequence header
	// 0x00 0x00 0x00
	// 0x01 configurationVersion
	// 0x42 AVCProfileIndication
	// 0x00 profile_compatibility
	// 0x1E AVCLevelIndication
	// 0xFF lengthSizeMinusOne
	RTMP_AVC_HEAD         = []byte{0x17, 0x00, 0x00, 0x00, 0x00, 0x01, 0x42, 0x00, 0x1E, 0xFF}
	RTMP_KEYFRAME_HEAD    = []byte{0x17, 0x01, 0x00, 0x00, 0x00}
	RTMP_NORMALFRAME_HEAD = []byte{0x27, 0x01, 0x00, 0x00, 0x00}
)
var NALU_SEI_BYTE []byte

// H.264/AVC视频编码标准中,整个系统框架被分为了两个层面:视频编码层面(VCL)和网络抽象层面(NAL)
// NAL - Network Abstract Layer
// raw byte sequence payload (RBSP) 原始字节序列载荷

// SplitH264 以0x00000001分割H264裸数据
func SplitH264(payload []byte) (nalus [][]byte) {
	for _, v := range bytes.SplitN(payload, NALU_Delimiter2, -1) {
		if len(v) == 0 {
			continue
		}
		nalus = append(nalus, bytes.SplitN(v, NALU_Delimiter1, -1)...)
	}
	return
}

func BuildH264SeqHeaderFromSpsPps(sps, pps []byte) (seqHeader []byte) {
	lenSPS, lenPPS := len(sps), len(pps)
	seqHeader = append([]byte{}, RTMP_AVC_HEAD...)
	if lenSPS > 3 {
		copy(seqHeader[6:], sps[1:4])
	}
	seqHeader = append(seqHeader, 0xE1, byte(lenSPS>>8), byte(lenSPS))
	seqHeader = append(seqHeader, sps...)
	seqHeader = append(append(seqHeader, 0x01, byte(lenPPS>>8), byte(lenPPS)), pps...)
	return
}

// ISO/IEC 14496-15 11(16)/page
//
// Advanced Video Coding
//

// AVCC
type AVCDecoderConfigurationRecord struct {
	ConfigurationVersion       byte // 8 bits Version
	AVCProfileIndication       byte // 8 bits
	ProfileCompatibility       byte // 8 bits
	AVCLevelIndication         byte // 8 bits
	Reserved1                  byte // 6 bits
	LengthSizeMinusOne         byte // 2 bits 非常重要,每个NALU包前面都(lengthSizeMinusOne & 3)+1个字节的NAL包长度描述
	Reserved2                  byte // 3 bits
	NumOfSequenceParameterSets byte // 5 bits SPS 的个数,计算方法是 numOfSequenceParameterSets & 0x1F
	NumOfPictureParameterSets  byte // 8 bits PPS 的个数

	SequenceParameterSetLength  uint16 // 16 byte SPS Length
	SequenceParameterSetNALUnit []byte // n byte  SPS
	PictureParameterSetLength   uint16 // 16 byte PPS Length
	PictureParameterSetNALUnit  []byte // n byte  PPS
}

func (p *AVCDecoderConfigurationRecord) Marshal(b []byte) (n int) {
	b[0] = 1
	b[1] = p.AVCProfileIndication
	b[2] = p.ProfileCompatibility
	b[3] = p.AVCLevelIndication
	b[4] = p.LengthSizeMinusOne | 0xfc
	b[5] = uint8(1) | 0xe0
	n += 6

	pio.PutU16BE(b[n:], p.SequenceParameterSetLength)
	n += 2
	copy(b[n:], p.SequenceParameterSetNALUnit)
	n += len(p.SequenceParameterSetNALUnit)
	b[n] = uint8(1)
	n++

	pio.PutU16BE(b[n:], p.PictureParameterSetLength)
	n += 2
	copy(b[n:], p.PictureParameterSetNALUnit)
	n += len(p.PictureParameterSetNALUnit)

	return
}

var ErrDecconfInvalid = errors.New("decode error")

func (p *AVCDecoderConfigurationRecord) Unmarshal(b []byte) (n int, err error) {
	if len(b) < 7 {
		err = errors.New("not enough len")
		return
	}

	p.AVCProfileIndication = b[1]
	p.ProfileCompatibility = b[2]
	p.AVCLevelIndication = b[3]
	p.LengthSizeMinusOne = b[4] & 0x03
	spscount := int(b[5] & 0x1f)
	n += 6
	var sps, pps [][]byte
	for i := 0; i < spscount; i++ {
		if len(b) < n+2 {
			err = ErrDecconfInvalid
			return
		}
		spslen := util.ReadBE[int](b[n : n+2])
		n += 2

		if len(b) < n+spslen {
			err = ErrDecconfInvalid
			return
		}
		sps = append(sps, b[n:n+spslen])
		n += spslen
	}
	p.SequenceParameterSetLength = uint16(len(sps[0]))
	p.SequenceParameterSetNALUnit = sps[0]
	if len(b) < n+1 {
		err = ErrDecconfInvalid
		return
	}
	ppscount := int(b[n])
	n++

	for i := 0; i < ppscount; i++ {
		if len(b) < n+2 {
			err = ErrDecconfInvalid
			return
		}
		ppslen := util.ReadBE[int](b[n : n+2])
		n += 2

		if len(b) < n+ppslen {
			err = ErrDecconfInvalid
			return
		}
		pps = append(pps, b[n:n+ppslen])
		n += ppslen
	}
	if ppscount >= 1 {
		p.PictureParameterSetLength = uint16(len(pps[0]))
		p.PictureParameterSetNALUnit = pps[0]
	} else {
		err = ErrDecconfInvalid
	}
	return
}

type NALUnit struct {
	NALUHeader
	RBSP
}

type NALUHeader struct {
	forbidden_zero_bit byte // 1 bit  0
	nal_ref_idc        byte // 2 bits nal_unit_type等于6,9,10,11或12的NAL单元其nal_ref_idc都应等于 0
	nal_uint_type      byte // 5 bits 包含在 NAL 单元中的 RBSP 数据结构的类型
}

type RBSP interface {
}

/*
0      Unspecified                                                    non-VCL
1      Coded slice of a non-IDR picture                               VCL
2      Coded slice data partition A                                   VCL
3      Coded slice data partition B                                   VCL
4      Coded slice data partition C                                   VCL
5      Coded slice of an IDR picture                                  VCL
6      Supplemental enhancement information (SEI)                     non-VCL
7      Sequence parameter set                                         non-VCL
8      Picture parameter set                                          non-VCL
9      Access unit delimiter                                          non-VCL
10     End of sequence                                                non-VCL
11     End of stream                                                  non-VCL
12     Filler data                                                    non-VCL
13     Sequence parameter set extension                               non-VCL
14     Prefix NAL unit                                                non-VCL
15     Subset sequence parameter set                                  non-VCL
16     Depth parameter set                                            non-VCL
17..18 Reserved                                                       non-VCL
19     Coded slice of an auxiliary coded picture without partitioning non-VCL
20     Coded slice extension                                          non-VCL
21     Coded slice extension for depth view components                non-VCL
22..23 Reserved                                                       non-VCL
24..31 Unspecified                                                    non-VCL

0:未规定
1:非IDR图像中不采用数据划分的片段
2:非IDR图像中A类数据划分片段
3:非IDR图像中B类数据划分片段
4:非IDR图像中C类数据划分片段
5:IDR图像的片段
6:补充增强信息（SEI）
7:序列参数集（SPS）
8:图像参数集（PPS）
9:分割符
10:序列结束符
11:流结束符
12:填充数据
13:序列参数集扩展
14:带前缀的NAL单元
15:子序列参数集
16 – 18:保留
19:不采用数据划分的辅助编码图像片段
20:编码片段扩展
21 – 23:保留
24 – 31:未规定

nal_unit_type		NAL类型						nal_reference_bit
0					未使用						0
1					非IDR的片					此片属于参考帧,则不等于0,不属于参考帧，则等与0
2					片数据A分区					同上
3					片数据B分区					同上
4					片数据C分区					同上
5					IDR图像的片					5
6					补充增强信息单元（SEI）		0
7					序列参数集					非0
8					图像参数集					非0
9					分界符						0
10					序列结束					0
11					码流结束					0
12					填充						0
13..23				保留						0
24..31				不保留						0
*/

func ReadPPS(w io.Writer) {

}
