package track

import (
	"time"

	"github.com/Monibuca/engine/v4/codec"
	. "github.com/Monibuca/engine/v4/common"
)

func NewAAC(stream IStream) (aac *AAC) {
	aac = &AAC{}
	aac.Name = "aac"
	aac.Stream = stream
	aac.CodecID = codec.CodecID_AAC
	aac.Init(stream, 32)
	aac.Poll = time.Millisecond * 20
	return
}

type AAC Audio

func (aac *AAC) WriteAVCC(ts uint32, frame AVCCFrame) {
	if frame.IsSequence() {
		aac.DecoderConfiguration.Reset()
		aac.DecoderConfiguration.AppendAVCC(frame)
		config1, config2 := frame[2], frame[3]
		//audioObjectType = (config1 & 0xF8) >> 3
		// 1 AAC MAIN 	ISO/IEC 14496-3 subpart 4
		// 2 AAC LC 	ISO/IEC 14496-3 subpart 4
		// 3 AAC SSR 	ISO/IEC 14496-3 subpart 4
		// 4 AAC LTP 	ISO/IEC 14496-3 subpart 4
		aac.Channels = ((config2 >> 3) & 0x0F) //声道
		aac.SampleRate = HZ(codec.SamplingFrequencies[((config1&0x7)<<1)|(config2>>7)])
		aac.DecoderConfiguration.AppendRaw(AudioSlice(frame[2:]))
	} else {
		(*Audio)(aac).WriteAVCC(ts, frame)
	}
}
