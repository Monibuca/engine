package track

import (
	"net"
	"time"

	"go.uber.org/zap"
	"m7s.live/engine/v4/codec"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/util"
)

var _ SpesificTrack[[]byte] = (*AAC)(nil)

func NewAAC(stream IStream) (aac *AAC) {
	aac = &AAC{
		sizeLength: 13,
		Mode:       2,
	}
	aac.CodecID = codec.CodecID_AAC
	aac.Channels = 2
	aac.SampleSize = 16
	aac.SetStuff("aac", stream, int(32), byte(97), aac, time.Millisecond*10)
	aac.AVCCHead = []byte{0xAF, 1}
	return
}

type AAC struct {
	Audio
	sizeLength int // 通常为13
	Mode       int // 1为lbr，2为hbr
	lack       int // 用于处理不完整的AU,缺少的字节数
}

// https://datatracker.ietf.org/doc/html/rfc3640#section-3.2.1
func (aac *AAC) WriteRTPFrame(frame *RTPFrame) {
	auHeaderLen := util.ReadBE[int](frame.Payload[:aac.Mode]) >> 3 //通常为2，即一个AU Header的长度
	// auHeaderCount := auHeaderLen >> 1 // AU Header的个数, 通常为1
	if auHeaderLen == 0 {
		aac.Value.AppendRaw(frame.Payload)
	} else {
		startOffset := aac.Mode + auHeaderLen // 实际数据开始的位置
		if aac.lack > 0 {
			rawLen := len(aac.Value.Raw)
			if rawLen == 0 {
				aac.Stream.Error("lack >0 but rawlen=0")
			}
			last := util.Buffer(aac.Value.Raw[rawLen-1])
			auLen := len(frame.Payload) - startOffset
			if aac.lack > auLen {
				last.Write(frame.Payload[startOffset:])
				aac.lack -= auLen
				return
			} else if aac.lack < auLen {
				aac.Stream.Warn("lack < auLen", zap.Int("lack", aac.lack), zap.Int("auLen", auLen))
			}
			last.Write(frame.Payload[startOffset : startOffset+aac.lack])
			aac.lack = 0
			return
		}
		for iIndex := aac.Mode; iIndex <= auHeaderLen; iIndex += aac.Mode {
			auLen := util.ReadBE[int](frame.Payload[iIndex:iIndex+aac.Mode]) >> (8*aac.Mode - aac.sizeLength) //取高13bit代表AU的长度
			nextPos := startOffset + auLen
			if len(frame.Payload) < nextPos {
				aac.lack = nextPos - len(frame.Payload)
				aac.Value.AppendRaw(frame.Payload[startOffset:])
				break
			} else {
				aac.Value.AppendRaw(frame.Payload[startOffset:nextPos])
			}
			startOffset = nextPos
		}
	}
}

func (aac *AAC) WriteAVCC(ts uint32, frame AVCCFrame) {
	if l := util.SizeOfBuffers(frame); l < 4 {
		aac.Stream.Error("AVCC data too short", zap.Int("len", l))
		return
	}
	if frame.IsSequence() {
		aac.Audio.DecoderConfiguration.AVCC = net.Buffers(frame)
		config1, config2 := frame[0][2], frame[0][3]
		aac.Profile = (config1 & 0xF8) >> 3
		aac.Channels = ((config2 >> 3) & 0x0F) //声道
		aac.Audio.SampleRate = uint32(codec.SamplingFrequencies[((config1&0x7)<<1)|(config2>>7)])
		aac.Audio.DecoderConfiguration.Raw = frame[0][2:]
		aac.Attach()
	} else {
		aac.Value.AppendRaw(frame[0][2:])
		for _, data := range frame[1:] {
			aac.Value.AppendRaw(data)
		}
		aac.Audio.WriteAVCC(ts, frame)
	}
}

func (aac *AAC) CompleteRTP(value *AVFrame[[]byte]) {
	l := util.SizeOfBuffers(value.Raw)
	//AU_HEADER_LENGTH,因为单位是bit, 除以8就是auHeader的字节长度；又因为单个auheader字节长度2字节，所以再除以2就是auheader的个数。
	auHeaderLen := []byte{0x00, 0x10, (byte)((l & 0x1fe0) >> 5), (byte)((l & 0x1f) << 3)} // 3 = 16-13, 5 = 8-3
	packets := util.SplitBuffers(value.Raw, 1200)
	for i, packet := range packets {
		expand := append(packet, nil)
		copy(expand[1:], packet)
		expand[0] = auHeaderLen
		packets[i] = expand
	}
	aac.PacketizeRTP(packets...)
}
