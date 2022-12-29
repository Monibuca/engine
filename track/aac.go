package track

import (
	"net"
	"time"

	"go.uber.org/zap"
	"m7s.live/engine/v4/codec"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/util"
)

func NewAAC(stream IStream) (aac *AAC) {
	aac = &AAC{}
	aac.CodecID = codec.CodecID_AAC
	aac.SampleSize = 16
	aac.SetStuff("aac", stream, int(32), byte(97), aac, time.Millisecond*10)
	aac.AVCCHead = []byte{0xAF, 1}
	return
}

type AAC struct {
	Audio
	buffer []byte
}

func (aac *AAC) writeRTPFrame(frame *RTPFrame) {
	aac.Audio.Media.AVRing.RingBuffer.Value.AppendRTP(frame)
	auHeaderLen := util.ReadBE[int](frame.Payload[:2]) >> 3
	startOffset := 2 + auHeaderLen
	if !frame.Marker {
		aac.buffer = append(aac.buffer, frame.Payload[startOffset:]...)
	} else {
		if aac.buffer != nil {
			aac.buffer = append(append([]byte{}, frame.Payload...), aac.buffer...)
		} else {
			aac.buffer = frame.Payload
		}
		for iIndex := 2; iIndex <= auHeaderLen; iIndex += 2 {
			auLen := util.ReadBE[int](aac.buffer[iIndex:iIndex+2]) >> 3
			aac.WriteSlice(aac.buffer[startOffset : startOffset+auLen])
			startOffset += auLen
		}
		aac.generateTimestamp()
		aac.Flush()
		aac.buffer = nil
	}
}

func (aac *AAC) WriteAVCC(ts uint32, frame AVCCFrame) {
	if len(frame) < 4 {
		aac.Audio.Stream.Error("AVCC data too short", zap.ByteString("data", frame))
		return
	}
	if frame.IsSequence() {
		aac.Audio.DecoderConfiguration.AVCC = net.Buffers{frame}
		config1, config2 := frame[2], frame[3]
		aac.Profile = (config1 & 0xF8) >> 3
		aac.Channels = ((config2 >> 3) & 0x0F) //声道
		aac.Audio.SampleRate = uint32(codec.SamplingFrequencies[((config1&0x7)<<1)|(config2>>7)])
		aac.Audio.DecoderConfiguration.Raw = AudioSlice(frame[2:])
		aac.Attach()
	} else {
		aac.WriteSlice(AudioSlice(frame[2:]))
		aac.Audio.WriteAVCC(ts, frame)
		aac.Flush()
	}
}

func (aac *AAC) Flush() {
	// RTP格式补完
	value := aac.Audio.Media.RingBuffer.Value
	if aac.ComplementRTP() {
		l := util.SizeOfBuffers(value.Raw)
		var packet = make(net.Buffers, len(value.Raw)+1)
		//AU_HEADER_LENGTH,因为单位是bit, 除以8就是auHeader的字节长度；又因为单个auheader字节长度2字节，所以再除以2就是auheader的个数。
		packet[0] = []byte{0x00, 0x10, (byte)((l & 0x1fe0) >> 5), (byte)((l & 0x1f) << 3)}
		for i, raw := range value.Raw {
			packet[i+1] = raw
		}
		packets := util.SplitBuffers(packet, 1200)
		aac.PacketizeRTP(packets...)
	}
	aac.Audio.Flush()
}
