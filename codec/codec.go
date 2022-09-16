package codec

import (
	"errors"

	"m7s.live/engine/v4/util"
)

type AudioCodecID byte
type VideoCodecID byte

const (
	ADTS_HEADER_SIZE              = 7
	CodecID_AAC      AudioCodecID = 0xA
	CodecID_PCMA     AudioCodecID = 7
	CodecID_PCMU     AudioCodecID = 8
	CodecID_H264     VideoCodecID = 7
	CodecID_H265     VideoCodecID = 0xC
)

func (codecId AudioCodecID) String() string {
	switch codecId {
	case CodecID_AAC:
		return "aac"
	case CodecID_PCMA:
		return "pcma"
	case CodecID_PCMU:
		return "pcmu"
	}
	return "unknow"
}

func (codecId VideoCodecID) String() string {
	switch codecId {
	case CodecID_H264:
		return "h264"
	case CodecID_H265:
		return "h265"
	}
	return "unknow"
}

// ISO/IEC 14496-3 38(52)/page
//
// Audio
//

type AudioSpecificConfig struct {
	AudioObjectType        byte // 5 bits
	SamplingFrequencyIndex byte // 4 bits
	ChannelConfiguration   byte // 4 bits
	GASpecificConfig
}

type GASpecificConfig struct {
	FrameLengthFlag    byte // 1 bit
	DependsOnCoreCoder byte // 1 bit
	ExtensionFlag      byte // 1 bit
}

//
// AudioObjectTypes -> ISO/IEC 14496-3 43(57)/page
//
// 1 AAC MAIN 	ISO/IEC 14496-3 subpart 4
// 2 AAC LC 	ISO/IEC 14496-3 subpart 4
// 3 AAC SSR 	ISO/IEC 14496-3 subpart 4
// 4 AAC LTP 	ISO/IEC 14496-3 subpart 4
//
//

// ISO/IEC 13838-7 20(25)/page
//
// Advanced Audio Coding
//
// AudioDataTransportStream
type ADTS struct {
	ADTSFixedHeader
	ADTSVariableHeader
}

// 28 bits
type ADTSFixedHeader struct {
	SyncWord               uint16 // 12 bits The bit string ‘1111 1111 1111’. See ISO/IEC 11172-3,subclause 2.4.2.3 (Table 8)
	ID                     byte   // 1 bit MPEG identifier, set to ‘1’. See ISO/IEC 11172-3,subclause 2.4.2.3 (Table 8)
	Layer                  byte   // 2 bits Indicates which layer is used. Set to ‘00’. See ISO/IEC 11172-3,subclause 2.4.2.3 (Table 8)
	ProtectionAbsent       byte   // 1 bit Indicates whether error_check() data is present or not. Same assyntax element ‘protection_bit’ in ISO/IEC 11172-3,subclause 2.4.1 and 2.4.2 (Table 8)
	Profile                byte   // 2 bits profile used. See clause 2 (Table 8)
	SamplingFrequencyIndex byte   // 4 bits indicates the sampling frequency used according to the followingtable (Table 8)
	PrivateBit             byte   // 1 bit see ISO/IEC 11172-3, subclause 2.4.2.3 (Table 8)
	ChannelConfiguration   byte   // 3 bits indicates the channel configuration used. Ifchannel_configuration is greater than 0, the channelconfiguration is given in Table 42, see subclause 8.5.3.1. Ifchannel_configuration equals 0, the channel configuration is notspecified in the header and must be given by aprogram_config_element() following as first syntactic element inthe first raw_data_block() after the header (seesubclause 8.5.3.2), or by the implicit configuration (seesubclause 8.5.3.3) or must be known in the application (Table 8)
	OriginalCopy           byte   // 1 bit see ISO/IEC 11172-3, definition of data element copyright
	Home                   byte   // 1 bit see ISO/IEC 11172-3, definition of data element original/copy
}

// SyncWord, 同步头 总是0xFFF, all bits must be 1，代表着一个ADTS帧的开始
// ID, MPEG Version: 0 for MPEG-4, 1 for MPEG-2
// Layer, always: '00'
// ProtectionAbsent, 表示是否误码校验
// Profile, 表示使用哪个级别的AAC，有些芯片只支持AAC LC 。在MPEG-2 AAC中定义了3种.
// SamplingFrequencyIndex, 表示使用的采样率下标，通过这个下标在 Sampling Frequencies[ ]数组中查找得知采样率的值
// PrivateBit,
// ChannelConfiguration, 表示声道数
// OriginalCopy,
// Home,

// Profile:
//
// 0: Main profile
// 1: Low Complexity profile(LC)
// 2: Scalable Sampling Rate profile(SSR)
// 3: Reserved
//
var SamplingFrequencies = [...]int{96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050, 16000, 12000, 11025, 8000, 7350, 0, 0, 0}

// Sampling Frequencies[]:
//
// 0: 96000 Hz
// 1: 88200 Hz
// 2: 64000 Hz
// 3: 48000 Hz
// 4: 44100 Hz
// 5: 32000 Hz
// 6: 24000 Hz
// 7: 22050 Hz
// 8: 16000 Hz
// 9: 12000 Hz
// 10: 11025 Hz
// 11: 8000 Hz
// 12: 7350 Hz
// 13: Reserved
// 14: Reserved
// 15: frequency is written explictly
//

// ChannelConfiguration:
//
// 0: Defined in AOT Specifc Config
// 1: 1 channel: front-center
// 2: 2 channels: front-left, front-right
// 3: 3 channels: front-center, front-left, front-right
// 4: 4 channels: front-center, front-left, front-right, back-center
// 5: 5 channels: front-center, front-left, front-right, back-left, back-right
// 6: 6 channels: front-center, front-left, front-right, back-left, back-right, LFE-channel
// 7: 8 channels: front-center, front-left, front-right, side-left, side-right, back-left, back-right, LFE-channel
// 8-15: Reserved
//

// 28 bits
type ADTSVariableHeader struct {
	CopyrightIdentificationBit   byte   // 1 bit One bit of the 72-bit copyright identification field (seecopyright_id above). The bits of this field are transmitted frame by frame; the first bit is indicated by the copyright_identification_start bit set to ‘1’. The field consists of an 8-bit copyright_identifier, followed by a 64-bit copyright_number.The copyright identifier is given by a Registration Authority as designated by SC29. The copyright_number is a value which identifies uniquely the copyrighted material. See ISO/IEC 13818-3, subclause 2.5.2.13 (Table 9)
	CopyrightIdentificationStart byte   // 1 bit One bit to indicate that the copyright_identification_bit in this audio frame is the first bit of the 72-bit copyright identification. If no copyright identification is transmitted, this bit should be kept '0'.'0' no start of copyright identification in this audio frame '1' start of copyright identification in this audio frame See ISO/IEC 13818-3, subclause 2.5.2.13 (Table 9)
	AACFrameLength               uint16 // 13 bits Length of the frame including headers and error_check in bytes(Table 9)
	ADTSBufferFullness           uint16 // 11 bits state of the bit reservoir in the course of encoding the ADTS frame, up to and including the first raw_data_block() and the optionally following adts_raw_data_block_error_check(). It is transmitted as the number of available bits in the bit reservoir divided by NCC divided by 32 and truncated to an integer value (Table 9). A value of hexadecimal 7FF signals that the bitstream is a variable rate bitstream. In this case, buffer fullness is not applicable
	NumberOfRawDataBlockInFrame  byte   // 2 bits Number of raw_data_block()’s that are multiplexed in the adts_frame() is equal to number_of_raw_data_blocks_in_frame + 1. The minimum value is 0 indicating 1 raw_data_block()(Table 9)
}

// CopyrightIdentificationBit,
// CopyrightIdentificationStart,
// AACFrameLength, 一个ADTS帧的长度包括ADTS头和raw data block.
// ADTSBufferFullness, 0x7FF 说明是码率可变的码流.
// NumberOfRawDataBlockInFrame, 表示ADTS帧中有number_of_raw_data_blocks_in_frame + 1个AAC原始帧

// 所以说number_of_raw_data_blocks_in_frame == 0 表示说ADTS帧中有一个AAC数据块并不是说没有。(一个AAC原始帧包含一段时间内1024个采样及相关数据)

func AudioSpecificConfigToADTS(asc AudioSpecificConfig, rawDataLength int) (adts ADTS, adtsByte []byte, err error) {
	if asc.ChannelConfiguration > 8 || asc.FrameLengthFlag > 13 {
		err = errors.New("Reserved field.")
		return
	}

	// ADTSFixedHeader
	adts.SyncWord = 0xfff
	adts.ID = 0
	adts.Layer = 0
	adts.ProtectionAbsent = 1

	// SyncWord(12) + ID(1) + Layer(2) + ProtectionAbsent(1)
	adtsByte = append(adtsByte, 0xff)
	adtsByte = append(adtsByte, 0xf1)

	if asc.AudioObjectType >= 3 || asc.AudioObjectType == 0 {
		adts.Profile = 1
	} else {
		adts.Profile = asc.AudioObjectType - 1
	}

	adts.SamplingFrequencyIndex = asc.SamplingFrequencyIndex
	adts.PrivateBit = 0
	adts.ChannelConfiguration = asc.ChannelConfiguration
	adts.OriginalCopy = 0
	adts.Home = 0

	// Profile(2) + SamplingFrequencyIndex(4) + PrivateBit(1) + ChannelConfiguration(3)(取高1位)
	byte3 := uint8(adts.Profile<<6) + uint8(adts.SamplingFrequencyIndex<<2) + uint8(adts.PrivateBit<<1) + uint8((adts.ChannelConfiguration&0x7)>>2)
	adtsByte = append(adtsByte, byte3)

	// ADTSVariableHeader
	adts.CopyrightIdentificationBit = 0
	adts.CopyrightIdentificationStart = 0
	adts.AACFrameLength = 7 + uint16(rawDataLength)
	adts.ADTSBufferFullness = 0x7ff
	adts.NumberOfRawDataBlockInFrame = 0

	// ChannelConfiguration(3)(取低2位) + OriginalCopy(1) + Home(1) + CopyrightIdentificationBit(1) + CopyrightIdentificationStart(1) +  AACFrameLength(13)(取高2位)
	byte4 := uint8((adts.ChannelConfiguration&0x3)<<6) + uint8((adts.AACFrameLength&0x1fff)>>11)
	adtsByte = append(adtsByte, byte4)

	// AACFrameLength(13)
	// xx xxxxxxxx xxx
	// 取中间的部分
	byte5 := uint8(((adts.AACFrameLength & 0x1fff) >> 3) & 0x0ff)
	adtsByte = append(adtsByte, byte5)

	// AACFrameLength(13)(取低3位) + ADTSBufferFullness(11)(取高5位)
	byte6 := uint8((adts.AACFrameLength&0x0007)<<5) + 0x1f
	adtsByte = append(adtsByte, byte6)

	// ADTSBufferFullness(11)(取低6位) + NumberOfRawDataBlockInFrame(2)
	adtsByte = append(adtsByte, 0xfc)

	return
}
func ParseRTPAAC(payload []byte) (result [][]byte) {
	auHeaderLen := util.ReadBE[int](payload[:2]) >> 3
	var auLenArray []int
	for iIndex := 2; iIndex <= auHeaderLen; iIndex += 2 {
		auLen := util.ReadBE[int](payload[iIndex:iIndex+2]) >> 3
		auLenArray = append(auLenArray, auLen)
	}
	startOffset := 2 + auHeaderLen
	for _, auLen := range auLenArray {
		endOffset := startOffset + auLen
		result = append(result, payload[startOffset:endOffset])
		startOffset = startOffset + auLen
	}
	return
}
