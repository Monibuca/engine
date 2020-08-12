package avformat

import (
	"io"

	"github.com/Monibuca/engine/v2/util"
)

// Start Code + NAL Unit -> NALU Header + NALU Body
// RTP Packet -> NALU Header + NALU Body

// NALU Body -> Slice Header + Slice data
// Slice data -> flags + Macroblock layer1 + Macroblock layer2 + ...
// Macroblock layer1 -> mb_type + PCM Data
// Macroblock layer2 -> mb_type + Sub_mb_pred or mb_pred + Residual Data
// Residual Data ->

const (
	// NALU Type
	NALU_Unspecified           = 0
	NALU_Non_IDR_Picture       = 1
	NALU_Data_Partition_A      = 2
	NALU_Data_Partition_B      = 3
	NALU_Data_Partition_C      = 4
	NALU_IDR_Picture           = 5
	NALU_SEI                   = 6
	NALU_SPS                   = 7
	NALU_PPS                   = 8
	NALU_Access_Unit_Delimiter = 9
	NALU_Sequence_End          = 10
	NALU_Stream_End            = 11
	NALU_Filler_Data           = 12
	NALU_SPS_Extension         = 13
	NALU_Prefix                = 14
	NALU_SPS_Subset            = 15
	NALU_DPS                   = 16
	NALU_Reserved1             = 17
	NALU_Reserved2             = 18
	NALU_Not_Auxiliary_Coded   = 19
	NALU_Coded_Slice_Extension = 20
	NALU_Reserved3             = 21
	NALU_Reserved4             = 22
	NALU_Reserved5             = 23
	NALU_STAPA                 = 24
	NALU_FUA                   = 28
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

type H264 struct {
	SPS []byte
	PPS []byte
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

//Payload 分包，用于RTP传输
func (h264 *H264) Payload(mtu int, payload []byte) (payloads [][]byte) {
	if payload == nil {
		return payloads
	}
	videoFrameType := payload[0] >> 4
	if videoFrameType == 1 || videoFrameType == 4 {
		payloads = append(payloads, h264.SPS, h264.PPS)
	}
	for nalu, naluLen := payload[5:], 4; len(nalu) > naluLen; naluLen = int(util.BigEndian.Uint32(nalu)) + 4 {
		nalu = nalu[naluLen:]
		naluType := nalu[0] & 0x1F   //00011111
		naluRefIdc := nalu[0] & 0x60 //1110000

		if naluType == 9 || naluType == 12 {
			continue
		}

		// Single NALU
		if len(nalu) <= mtu {
			out := make([]byte, len(nalu))
			copy(out, nalu)
			payloads = append(payloads, out)
			continue
		}

		// FU-A
		maxFragmentSize := mtu - 2

		// The FU payload consists of fragments of the payload of the fragmented
		// NAL unit so that if the fragmentation unit payloads of consecutive
		// FUs are sequentially concatenated, the payload of the fragmented NAL
		// unit can be reconstructed.  The NAL unit type octet of the fragmented
		// NAL unit is not included as such in the fragmentation unit payload,
		// 	but rather the information of the NAL unit type octet of the
		// fragmented NAL unit is conveyed in the F and NRI fields of the FU
		// indicator octet of the fragmentation unit and in the type field of
		// the FU header.  An FU payload MAY have any number of octets and MAY
		// be empty.

		naluData := nalu
		// According to the RFC, the first octet is skipped due to redundant information
		naluDataIndex := 1
		naluDataLength := len(nalu) - naluDataIndex
		naluDataRemaining := naluDataLength

		if min(maxFragmentSize, naluDataRemaining) <= 0 {
			continue
		}

		for naluDataRemaining > 0 {
			currentFragmentSize := min(maxFragmentSize, naluDataRemaining)
			out := make([]byte, 2+currentFragmentSize)

			// +---------------+
			// |0|1|2|3|4|5|6|7|
			// +-+-+-+-+-+-+-+-+
			// |F|NRI|  Type   |
			// +---------------+
			out[0] = NALU_FUA | naluRefIdc

			// +---------------+
			//|0|1|2|3|4|5|6|7|
			//+-+-+-+-+-+-+-+-+
			//|S|E|R|  Type   |
			//+---------------+

			out[1] = naluType
			if naluDataRemaining == naluDataLength {
				// Set start bit
				out[1] |= 1 << 7
			} else if naluDataRemaining-currentFragmentSize == 0 {
				// Set end bit
				out[1] |= 1 << 6
			}

			copy(out[2:], naluData[naluDataIndex:naluDataIndex+currentFragmentSize])
			payloads = append(payloads, out)

			naluDataRemaining -= currentFragmentSize
			naluDataIndex += currentFragmentSize
		}

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
