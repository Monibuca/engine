package codec

import (
	"bytes"
	"errors"

	"github.com/q191201771/naza/pkg/nazabits"
	"m7s.live/engine/v4/util"
)

type H265NALUType byte

func (H265NALUType) Parse(b byte) H265NALUType {
	return H265NALUType(b & 0x7E >> 1)
}

const (
	// HEVC_VPS    = 0x40
	// HEVC_SPS    = 0x42
	// HEVC_PPS    = 0x44
	// HEVC_SEI    = 0x4E
	// HEVC_IDR    = 0x26
	// HEVC_PSLICE = 0x02

	NAL_UNIT_CODED_SLICE_TRAIL_N H265NALUType = iota // 0
	NAL_UNIT_CODED_SLICE_TRAIL_R                     // 1
	NAL_UNIT_CODED_SLICE_TSA_N                       // 2
	NAL_UNIT_CODED_SLICE_TLA                         // 3 // Current name in the spec: TSA_R
	NAL_UNIT_CODED_SLICE_STSA_N                      // 4
	NAL_UNIT_CODED_SLICE_STSA_R                      // 5
	NAL_UNIT_CODED_SLICE_RADL_N                      // 6
	NAL_UNIT_CODED_SLICE_DLP                         // 7 // Current name in the spec: RADL_R
	NAL_UNIT_CODED_SLICE_RASL_N                      // 8
	NAL_UNIT_CODED_SLICE_TFD                         // 9 // Current name in the spec: RASL_R
	NAL_UNIT_RESERVED_10
	NAL_UNIT_RESERVED_11
	NAL_UNIT_RESERVED_12
	NAL_UNIT_RESERVED_13
	NAL_UNIT_RESERVED_14
	NAL_UNIT_RESERVED_15
	NAL_UNIT_CODED_SLICE_BLA      // 16 // Current name in the spec: BLA_W_LP
	NAL_UNIT_CODED_SLICE_BLANT    // 17 // Current name in the spec: BLA_W_DLP
	NAL_UNIT_CODED_SLICE_BLA_N_LP // 18
	NAL_UNIT_CODED_SLICE_IDR      // 19// Current name in the spec: IDR_W_DLP
	NAL_UNIT_CODED_SLICE_IDR_N_LP // 20
	NAL_UNIT_CODED_SLICE_CRA      // 21
	NAL_UNIT_RESERVED_22
	NAL_UNIT_RESERVED_23
	NAL_UNIT_RESERVED_24
	NAL_UNIT_RESERVED_25
	NAL_UNIT_RESERVED_26
	NAL_UNIT_RESERVED_27
	NAL_UNIT_RESERVED_28
	NAL_UNIT_RESERVED_29
	NAL_UNIT_RESERVED_30
	NAL_UNIT_RESERVED_31
	NAL_UNIT_VPS                   // 32
	NAL_UNIT_SPS                   // 33
	NAL_UNIT_PPS                   // 34
	NAL_UNIT_ACCESS_UNIT_DELIMITER // 35
	NAL_UNIT_EOS                   // 36
	NAL_UNIT_EOB                   // 37
	NAL_UNIT_FILLER_DATA           // 38
	NAL_UNIT_SEI                   // 39 Prefix SEI
	NAL_UNIT_SEI_SUFFIX            // 40 Suffix SEI
	NAL_UNIT_RESERVED_41
	NAL_UNIT_RESERVED_42
	NAL_UNIT_RESERVED_43
	NAL_UNIT_RESERVED_44
	NAL_UNIT_RESERVED_45
	NAL_UNIT_RESERVED_46
	NAL_UNIT_RESERVED_47
	NAL_UNIT_RTP_AP
	NAL_UNIT_RTP_FU
	NAL_UNIT_UNSPECIFIED_50
	NAL_UNIT_UNSPECIFIED_51
	NAL_UNIT_UNSPECIFIED_52
	NAL_UNIT_UNSPECIFIED_53
	NAL_UNIT_UNSPECIFIED_54
	NAL_UNIT_UNSPECIFIED_55
	NAL_UNIT_UNSPECIFIED_56
	NAL_UNIT_UNSPECIFIED_57
	NAL_UNIT_UNSPECIFIED_58
	NAL_UNIT_UNSPECIFIED_59
	NAL_UNIT_UNSPECIFIED_60
	NAL_UNIT_UNSPECIFIED_61
	NAL_UNIT_UNSPECIFIED_62
	NAL_UNIT_UNSPECIFIED_63
	NAL_UNIT_INVALID
)

var AudNalu = []byte{0x00, 0x00, 0x00, 0x01, 0x46, 0x01, 0x10}
var ErrHevc = errors.New("hevc parse config error")

//HVCC
type HVCDecoderConfigurationRecord struct {
	PicWidthInLumaSamples  uint32 // sps
	PicHeightInLumaSamples uint32 // sps

	configurationVersion uint8

	generalProfileSpace              uint8
	generalTierFlag                  uint8
	generalProfileIdc                uint8
	generalProfileCompatibilityFlags uint32
	generalConstraintIndicatorFlags  uint64
	generalLevelIdc                  uint8

	lengthSizeMinusOne uint8

	numTemporalLayers uint8
	temporalIdNested  uint8

	chromaFormat         uint8
	bitDepthLumaMinus8   uint8
	bitDepthChromaMinus8 uint8
}

func ParseVpsSpsPpsFromSeqHeaderWithoutMalloc(payload []byte) (vps, sps, pps []byte, err error) {
	if len(payload) < 5 {
		return nil, nil, nil, ErrHevc
	}

	if payload[0] != 0x1c || payload[1] != 0x00 || payload[2] != 0 || payload[3] != 0 || payload[4] != 0 {
		return nil, nil, nil, ErrHevc
	}

	if len(payload) < 33 {
		return nil, nil, nil, ErrHevc
	}

	index := 27
	if numOfArrays := payload[index]; numOfArrays != 3 && numOfArrays != 4 {
		return nil, nil, nil, ErrHevc
	}
	index++

	if payload[index] != byte(NAL_UNIT_VPS)&0x3f {
		return nil, nil, nil, ErrHevc
	}
	if numNalus := util.ReadBE[int](payload[index+1 : index+3]); numNalus != 1 {
		return nil, nil, nil, ErrHevc
	}
	vpsLen := util.ReadBE[int](payload[index+3 : index+5])

	if len(payload) < 33+vpsLen {
		return nil, nil, nil, ErrHevc
	}

	vps = payload[index+5 : index+5+vpsLen]
	index += 5 + vpsLen

	if len(payload) < 38+vpsLen {
		return nil, nil, nil, ErrHevc
	}
	if payload[index] != byte(NAL_UNIT_SPS)&0x3f {
		return nil, nil, nil, ErrHevc
	}
	if numNalus := util.ReadBE[int](payload[index+1 : index+3]); numNalus != 1 {
		return nil, nil, nil, ErrHevc
	}
	spsLen := util.ReadBE[int](payload[index+3 : index+5])
	if len(payload) < 38+vpsLen+spsLen {
		return nil, nil, nil, ErrHevc
	}
	sps = payload[index+5 : index+5+spsLen]
	index += 5 + spsLen

	if len(payload) < 43+vpsLen+spsLen {
		return nil, nil, nil, ErrHevc
	}
	if payload[index] != byte(NAL_UNIT_PPS)&0x3f {
		return nil, nil, nil, ErrHevc
	}
	if numNalus := util.ReadBE[int](payload[index+1 : index+3]); numNalus != 1 {
		return nil, nil, nil, ErrHevc
	}
	ppsLen := util.ReadBE[int](payload[index+3 : index+5])
	if len(payload) < 43+vpsLen+spsLen+ppsLen {
		return nil, nil, nil, ErrHevc
	}
	pps = payload[index+5 : index+5+ppsLen]

	return
}
func BuildH265SeqHeaderFromVpsSpsPps(vps, sps, pps []byte) ([]byte, error) {
	sh := make([]byte, 43+len(vps)+len(sps)+len(pps))
	sh[0] = 0x1c
	sh[1] = 0x0
	sh[2] = 0x0
	sh[3] = 0x0
	sh[4] = 0x0

	// unsigned int(8) configurationVersion = 1;
	sh[5] = 0x1

	ctx := HVCDecoderConfigurationRecord{
		configurationVersion:             1,
		lengthSizeMinusOne:               3, // 4 bytes
		generalProfileCompatibilityFlags: 0xffffffff,
		generalConstraintIndicatorFlags:  0xffffffffffff,
	}
	if err := ctx.ParseVps(vps); err != nil {
		return nil, err
	}
	if err := ctx.ParseSps(sps); err != nil {
		return nil, err
	}

	// unsigned int(2) general_profile_space;
	// unsigned int(1) general_tier_flag;
	// unsigned int(5) general_profile_idc;
	sh[6] = ctx.generalProfileSpace<<6 | ctx.generalTierFlag<<5 | ctx.generalProfileIdc
	// unsigned int(32) general_profile_compatibility_flags
	util.PutBE(sh[7:7+4], ctx.generalProfileCompatibilityFlags)
	// unsigned int(48) general_constraint_indicator_flags
	util.PutBE(sh[11:11+4], uint32(ctx.generalConstraintIndicatorFlags>>16))
	util.PutBE(sh[15:15+2], uint16(ctx.generalConstraintIndicatorFlags))
	// unsigned int(8) general_level_idc;
	sh[17] = ctx.generalLevelIdc

	// bit(4) reserved = ‘1111’b;
	// unsigned int(12) min_spatial_segmentation_idc;
	// bit(6) reserved = ‘111111’b;
	// unsigned int(2) parallelismType;
	// TODO chef: 这两个字段没有解析
	util.PutBE(sh[18:20], 0xf000)
	sh[20] = 0xfc

	// bit(6) reserved = ‘111111’b;
	// unsigned int(2) chromaFormat;
	sh[21] = ctx.chromaFormat | 0xfc

	// bit(5) reserved = ‘11111’b;
	// unsigned int(3) bitDepthLumaMinus8;
	sh[22] = ctx.bitDepthLumaMinus8 | 0xf8

	// bit(5) reserved = ‘11111’b;
	// unsigned int(3) bitDepthChromaMinus8;
	sh[23] = ctx.bitDepthChromaMinus8 | 0xf8

	// bit(16) avgFrameRate;
	util.PutBE(sh[24:26], 0)

	// bit(2) constantFrameRate;
	// bit(3) numTemporalLayers;
	// bit(1) temporalIdNested;
	// unsigned int(2) lengthSizeMinusOne;
	sh[26] = 0<<6 | ctx.numTemporalLayers<<3 | ctx.temporalIdNested<<2 | ctx.lengthSizeMinusOne

	// num of vps sps pps
	sh[27] = 0x03
	i := 28
	sh[i] = byte(NAL_UNIT_VPS)
	// num of vps
	util.PutBE(sh[i+1:i+3], 1)
	// length
	util.PutBE(sh[i+3:i+5], len(vps))
	copy(sh[i+5:], vps)
	i = i + 5 + len(vps)
	sh[i] = byte(NAL_UNIT_SPS)
	util.PutBE(sh[i+1:i+3], 1)
	util.PutBE(sh[i+3:i+5], len(sps))
	copy(sh[i+5:], sps)
	i = i + 5 + len(sps)
	sh[i] = byte(NAL_UNIT_PPS)
	util.PutBE(sh[i+1:i+3], 1)
	util.PutBE(sh[i+3:i+5], len(pps))
	copy(sh[i+5:], pps)

	return sh, nil
}
func (ctx *HVCDecoderConfigurationRecord) ParseVps(vps []byte) error {
	if len(vps) < 2 {
		return ErrHevc
	}

	rbsp := nal2rbsp(vps[2:])
	br := nazabits.NewBitReader(rbsp)

	// skip
	// vps_video_parameter_set_id u(4)
	// vps_reserved_three_2bits   u(2)
	// vps_max_layers_minus1      u(6)
	if _, err := br.ReadBits16(12); err != nil {
		return ErrHevc
	}

	vpsMaxSubLayersMinus1, err := br.ReadBits8(3)
	if err != nil {
		return ErrHevc
	}
	if vpsMaxSubLayersMinus1+1 > ctx.numTemporalLayers {
		ctx.numTemporalLayers = vpsMaxSubLayersMinus1 + 1
	}

	// skip
	// vps_temporal_id_nesting_flag u(1)
	// vps_reserved_0xffff_16bits   u(16)
	if _, err := br.ReadBits32(17); err != nil {
		return ErrHevc
	}

	return ctx.parsePtl(&br, vpsMaxSubLayersMinus1)
}

func (ctx *HVCDecoderConfigurationRecord) ParseSps(sps []byte) error {
	var err error

	if len(sps) < 2 {
		return ErrHevc
	}

	rbsp := nal2rbsp(sps[2:])
	br := nazabits.NewBitReader(rbsp)

	// sps_video_parameter_set_id
	if _, err = br.ReadBits8(4); err != nil {
		return err
	}

	spsMaxSubLayersMinus1, err := br.ReadBits8(3)
	if err != nil {
		return err
	}

	if spsMaxSubLayersMinus1+1 > ctx.numTemporalLayers {
		ctx.numTemporalLayers = spsMaxSubLayersMinus1 + 1
	}

	// sps_temporal_id_nesting_flag
	if ctx.temporalIdNested, err = br.ReadBit(); err != nil {
		return err
	}

	if err = ctx.parsePtl(&br, spsMaxSubLayersMinus1); err != nil {
		return err
	}

	// sps_seq_parameter_set_id
	if _, err = br.ReadGolomb(); err != nil {
		return err
	}

	var cf uint32
	if cf, err = br.ReadGolomb(); err != nil {
		return err
	}
	ctx.chromaFormat = uint8(cf)
	if ctx.chromaFormat == 3 {
		if _, err = br.ReadBit(); err != nil {
			return err
		}
	}

	if ctx.PicWidthInLumaSamples, err = br.ReadGolomb(); err != nil {
		return err
	}
	if ctx.PicHeightInLumaSamples, err = br.ReadGolomb(); err != nil {
		return err
	}

	conformanceWindowFlag, err := br.ReadBit()
	if err != nil {
		return err
	}
	if conformanceWindowFlag != 0 {
		if _, err = br.ReadGolomb(); err != nil {
			return err
		}
		if _, err = br.ReadGolomb(); err != nil {
			return err
		}
		if _, err = br.ReadGolomb(); err != nil {
			return err
		}
		if _, err = br.ReadGolomb(); err != nil {
			return err
		}
	}

	var bdlm8 uint32
	if bdlm8, err = br.ReadGolomb(); err != nil {
		return err
	}
	ctx.bitDepthLumaMinus8 = uint8(bdlm8)
	var bdcm8 uint32
	if bdcm8, err = br.ReadGolomb(); err != nil {
		return err
	}
	ctx.bitDepthChromaMinus8 = uint8(bdcm8)

	_, err = br.ReadGolomb()
	if err != nil {
		return err
	}
	spsSubLayerOrderingInfoPresentFlag, err := br.ReadBit()
	if err != nil {
		return err
	}
	var i uint8
	if spsSubLayerOrderingInfoPresentFlag != 0 {
		i = 0
	} else {
		i = spsMaxSubLayersMinus1
	}
	for ; i <= spsMaxSubLayersMinus1; i++ {
		if _, err = br.ReadGolomb(); err != nil {
			return err
		}
		if _, err = br.ReadGolomb(); err != nil {
			return err
		}
		if _, err = br.ReadGolomb(); err != nil {
			return err
		}
	}

	if _, err = br.ReadGolomb(); err != nil {
		return err
	}
	if _, err = br.ReadGolomb(); err != nil {
		return err
	}
	if _, err = br.ReadGolomb(); err != nil {
		return err
	}
	if _, err = br.ReadGolomb(); err != nil {
		return err
	}
	if _, err = br.ReadGolomb(); err != nil {
		return err
	}
	if _, err = br.ReadGolomb(); err != nil {
		return err
	}

	return nil
}

func (ctx *HVCDecoderConfigurationRecord) parsePtl(br *nazabits.BitReader, maxSubLayersMinus1 uint8) error {
	var err error
	var ptl HVCDecoderConfigurationRecord
	if ptl.generalProfileSpace, err = br.ReadBits8(2); err != nil {
		return err
	}
	if ptl.generalTierFlag, err = br.ReadBit(); err != nil {
		return err
	}
	if ptl.generalProfileIdc, err = br.ReadBits8(5); err != nil {
		return err
	}
	if ptl.generalProfileCompatibilityFlags, err = br.ReadBits32(32); err != nil {
		return err
	}
	if ptl.generalConstraintIndicatorFlags, err = br.ReadBits64(48); err != nil {
		return err
	}
	if ptl.generalLevelIdc, err = br.ReadBits8(8); err != nil {
		return err
	}
	ctx.updatePtl(&ptl)

	if maxSubLayersMinus1 == 0 {
		return nil
	}

	subLayerProfilePresentFlag := make([]uint8, maxSubLayersMinus1)
	subLayerLevelPresentFlag := make([]uint8, maxSubLayersMinus1)
	for i := uint8(0); i < maxSubLayersMinus1; i++ {
		if subLayerProfilePresentFlag[i], err = br.ReadBit(); err != nil {
			return err
		}
		if subLayerLevelPresentFlag[i], err = br.ReadBit(); err != nil {
			return err
		}
	}
	if maxSubLayersMinus1 > 0 {
		for i := maxSubLayersMinus1; i < 8; i++ {
			if _, err = br.ReadBits8(2); err != nil {
				return err
			}
		}
	}

	for i := uint8(0); i < maxSubLayersMinus1; i++ {
		if subLayerProfilePresentFlag[i] != 0 {
			if _, err = br.ReadBits32(32); err != nil {
				return err
			}
			if _, err = br.ReadBits32(32); err != nil {
				return err
			}
			if _, err = br.ReadBits32(24); err != nil {
				return err
			}
		}

		if subLayerLevelPresentFlag[i] != 0 {
			if _, err = br.ReadBits8(8); err != nil {
				return err
			}
		}
	}

	return nil
}

func (ctx *HVCDecoderConfigurationRecord) updatePtl(ptl *HVCDecoderConfigurationRecord) {
	ctx.generalProfileSpace = ptl.generalProfileSpace

	if ptl.generalTierFlag > ctx.generalTierFlag {
		ctx.generalLevelIdc = ptl.generalLevelIdc

		ctx.generalTierFlag = ptl.generalTierFlag
	} else {
		if ptl.generalLevelIdc > ctx.generalLevelIdc {
			ctx.generalLevelIdc = ptl.generalLevelIdc
		}
	}

	if ptl.generalProfileIdc > ctx.generalProfileIdc {
		ctx.generalProfileIdc = ptl.generalProfileIdc
	}

	ctx.generalProfileCompatibilityFlags &= ptl.generalProfileCompatibilityFlags

	ctx.generalConstraintIndicatorFlags &= ptl.generalConstraintIndicatorFlags
}

func nal2rbsp(nal []byte) []byte {
	// TODO chef:
	// 1. 输出应该可由外部申请
	// 2. 替换性能
	// 3. 该函数应该放入avc中
	return bytes.Replace(nal, []byte{0x0, 0x0, 0x3}, []byte{0x0, 0x0}, -1)
}
