package track

import (
	"net"
	"time"

	"github.com/Monibuca/engine/v4/codec"
	. "github.com/Monibuca/engine/v4/common"
	"github.com/Monibuca/engine/v4/config"
	"github.com/Monibuca/engine/v4/util"
)

func NewAAC(stream IStream) (aac *AAC) {
	aac = &AAC{}
	aac.Name = "aac"
	aac.Stream = stream
	aac.CodecID = codec.CodecID_AAC
	aac.Init(stream, 32)
	aac.Poll = time.Millisecond * 20
	aac.DecoderConfiguration.PayloadType = 97
	if config.Global.RTPReorder {
		aac.orderQueue = make([]*RTPFrame, 20)
	}
	return
}

type AAC struct {
	Audio
}

func (aac *AAC) WriteRTP(raw []byte) {
	for frame := aac.UnmarshalRTP(raw); frame != nil; frame = aac.nextRTPFrame() {
		for _, payload := range codec.ParseRTPAAC(frame.Payload) {
			aac.WriteSlice(payload)
		}
		aac.Value.AppendRTP(frame)
		if frame.Marker {
			aac.generateTimestamp()
			aac.Flush()
		}
	}
}

func (aac *AAC) WriteAVCC(ts uint32, frame AVCCFrame) {
	if frame.IsSequence() {
		aac.DecoderConfiguration.AVCC = AudioSlice(frame)
		config1, config2 := frame[2], frame[3]
		//audioObjectType = (config1 & 0xF8) >> 3
		// 1 AAC MAIN 	ISO/IEC 14496-3 subpart 4
		// 2 AAC LC 	ISO/IEC 14496-3 subpart 4
		// 3 AAC SSR 	ISO/IEC 14496-3 subpart 4
		// 4 AAC LTP 	ISO/IEC 14496-3 subpart 4
		aac.Channels = ((config2 >> 3) & 0x0F) //声道
		aac.SampleRate = uint32(codec.SamplingFrequencies[((config1&0x7)<<1)|(config2>>7)])
		aac.DecoderConfiguration.Raw = AudioSlice(frame[2:])
		aac.DecoderConfiguration.FLV = net.Buffers{adcflv1, frame, adcflv2}
	} else {
		aac.Audio.WriteAVCC(ts, frame)
		aac.Flush()
	}
}

func (aac *AAC) Flush() {
	// RTP格式补完
	// TODO: MTU 分割
	if aac.Value.RTP == nil && config.Global.EnableRTP {
		l := util.SizeOfBuffers(aac.Value.Raw)
		o := make([]byte, 4, l+4)
		//AU_HEADER_LENGTH,因为单位是bit, 除以8就是auHeader的字节长度；又因为单个auheader字节长度2字节，所以再除以2就是auheader的个数。
		o[0] = 0x00 //高位
		o[1] = 0x10 //低位
		//AU_HEADER
		o[2] = (byte)((l & 0x1fe0) >> 5) //高位
		o[3] = (byte)((l & 0x1f) << 3)   //低位
		for _, raw := range aac.Value.Raw {
			o = append(o, raw...)
		}
		aac.PacketizeRTP(o)
	}
	aac.Audio.Flush()
}
