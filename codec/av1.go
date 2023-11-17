package codec

import (
	"errors"
	"io"
)

var (
	ErrInvalidMarker       = errors.New("invalid marker value found in AV1CodecConfigurationRecord")
	ErrInvalidVersion      = errors.New("unsupported AV1CodecConfigurationRecord version")
	ErrNonZeroReservedBits = errors.New("non-zero reserved bits found in AV1CodecConfigurationRecord")
)

const (
	AV1_OBU_SEQUENCE_HEADER        = 1
	AV1_OBU_TEMPORAL_DELIMITER     = 2
	AV1_OBU_FRAME_HEADER           = 3
	AV1_OBU_TILE_GROUP             = 4
	AV1_OBU_METADATA               = 5
	AV1_OBU_FRAME                  = 6
	AV1_OBU_REDUNDANT_FRAME_HEADER = 7
	AV1_OBU_TILE_LIST              = 8
	AV1_OBU_PADDING                = 15
)

type AV1CodecConfigurationRecord struct {
	Version                          byte
	SeqProfile                       byte
	SeqLevelIdx0                     byte
	SeqTier0                         byte
	HighBitdepth                     byte
	TwelveBit                        byte
	MonoChrome                       byte
	ChromaSubsamplingX               byte
	ChromaSubsamplingY               byte
	ChromaSamplePosition             byte
	InitialPresentationDelayPresent  byte
	InitialPresentationDelayMinusOne byte
	ConfigOBUs                       []byte
}

func (p *AV1CodecConfigurationRecord) Unmarshal(data []byte) (n int, err error) {
	l := len(data)
	if l < 4 {
		err = io.ErrShortWrite
		return
	}
	Marker := data[0] >> 7
	if Marker != 1 {
		return 0, ErrInvalidMarker
	}
	p.Version = data[0] & 0x7F
	if p.Version != 1 {
		return 1, ErrInvalidVersion
	}
	p.SeqProfile = data[1] >> 5
	p.SeqLevelIdx0 = data[1] & 0x1F
	p.SeqTier0 = data[2] >> 7
	p.HighBitdepth = (data[2] >> 6) & 0x01
	p.TwelveBit = (data[2] >> 5) & 0x01
	p.MonoChrome = (data[2] >> 4) & 0x01
	p.ChromaSubsamplingX = (data[2] >> 3) & 0x01
	p.ChromaSubsamplingY = (data[2] >> 2) & 0x01
	p.ChromaSamplePosition = data[2] & 0x03
	if data[3]>>5 != 0 {
		return 3, ErrNonZeroReservedBits
	}
	p.InitialPresentationDelayPresent = (data[3] >> 4) & 0x01
	if p.InitialPresentationDelayPresent == 1 {
		p.InitialPresentationDelayMinusOne = data[3] & 0x0F
	} else {
		if data[3]&0x0F != 0 {
			return 3, ErrNonZeroReservedBits
		}
		p.InitialPresentationDelayMinusOne = 0
	}
	if l > 4 {
		p.ConfigOBUs = data[4:]
	}

	return l, nil
}
