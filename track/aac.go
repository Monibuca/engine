package track

import (
	"io"
	"net"
	"time"

	"go.uber.org/zap"
	"m7s.live/engine/v4/codec"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/util"
)

var _ SpesificTrack = (*AAC)(nil)

func NewAAC(stream IStream, stuff ...any) (aac *AAC) {
	aac = &AAC{
		SizeLength: 13,
		Mode:       2,
	}
	aac.CodecID = codec.CodecID_AAC
	aac.Channels = 2
	aac.SampleSize = 16
	aac.SetStuff("aac", stream, int(256+128), byte(97), aac, time.Millisecond*10)
	aac.SetStuff(stuff...)
	aac.AVCCHead = []byte{0xAF, 1}
	return
}

type AAC struct {
	Audio
	SizeLength int // 通常为13
	Mode       int // 1为lbr，2为hbr
	lack       int // 用于处理不完整的AU,缺少的字节数
}

func (aac *AAC) WriteADTS(ts uint32, adts []byte) {
	if aac.SequenceHead == nil {
		profile := ((adts[2] & 0xc0) >> 6) + 1
		sampleRate := (adts[2] & 0x3c) >> 2
		channel := ((adts[2] & 0x1) << 2) | ((adts[3] & 0xc0) >> 6)
		config1 := (profile << 3) | ((sampleRate & 0xe) >> 1)
		config2 := ((sampleRate & 0x1) << 7) | (channel << 3)
		aac.Media.WriteSequenceHead([]byte{0xAF, 0x00, config1, config2})
		aac.SampleRate = uint32(codec.SamplingFrequencies[sampleRate])
		aac.Channels = channel
		aac.Parse(aac.SequenceHead[2:])
		aac.Attach()
	}
	aac.generateTimestamp(ts)
	frameLen := (int(adts[3]&3) << 11) | (int(adts[4]) << 3) | (int(adts[5]) >> 5)
	for len(adts) >= frameLen {
		aac.Value.AUList.Push(aac.BytesPool.GetShell(adts[7:frameLen]))
		adts = adts[frameLen:]
		if len(adts) < 7 {
			break
		}
		frameLen = (int(adts[3]&3) << 11) | (int(adts[4]) << 3) | (int(adts[5]) >> 5)
	}
	aac.Flush()
}

// https://datatracker.ietf.org/doc/html/rfc3640#section-3.2.1
func (aac *AAC) WriteRTPFrame(frame *RTPFrame) {
	auHeaderLen := util.ReadBE[int](frame.Payload[:aac.Mode]) >> 3 //通常为2，即一个AU Header的长度
	// auHeaderCount := auHeaderLen >> 1 // AU Header的个数, 通常为1
	if auHeaderLen == 0 {
		aac.Value.AUList.Push(aac.BytesPool.GetShell(frame.Payload))
	} else {
		startOffset := aac.Mode + auHeaderLen // 实际数据开始的位置
		if aac.lack > 0 {
			rawLen := aac.Value.AUList.ByteLength
			if rawLen == 0 {
				aac.Error("lack >0 but rawlen=0")
			}
			last := aac.Value.AUList.Pre
			auLen := len(frame.Payload) - startOffset
			if aac.lack > auLen {
				last.Value.Push(aac.BytesPool.GetShell(frame.Payload[startOffset:]))
				aac.lack -= auLen
				return
			} else if aac.lack < auLen {
				aac.Warn("lack < auLen", zap.Int("lack", aac.lack), zap.Int("auLen", auLen))
			}
			last.Value.Push(aac.BytesPool.GetShell(frame.Payload[startOffset : startOffset+aac.lack]))
			aac.lack = 0
			return
		}
		for iIndex := aac.Mode; iIndex <= auHeaderLen; iIndex += aac.Mode {
			auLen := util.ReadBE[int](frame.Payload[iIndex:iIndex+aac.Mode]) >> (8*aac.Mode - aac.SizeLength) //取高13bit代表AU的长度
			nextPos := startOffset + auLen
			if len(frame.Payload) < nextPos {
				aac.lack = nextPos - len(frame.Payload)
				aac.AppendAuBytes(frame.Payload[startOffset:])
				break
			} else {
				aac.AppendAuBytes(frame.Payload[startOffset:nextPos])
			}
			startOffset = nextPos
		}
	}
}

func (aac *AAC) WriteSequenceHead(sh []byte) {
	aac.Media.WriteSequenceHead(sh)
	config1, config2 := aac.SequenceHead[2], aac.SequenceHead[3]
	aac.Channels = ((config2 >> 3) & 0x0F) //声道
	aac.SampleRate = uint32(codec.SamplingFrequencies[((config1&0x7)<<1)|(config2>>7)])
	aac.Parse(aac.SequenceHead[2:])
	aac.Attach()
}

func (aac *AAC) WriteAVCC(ts uint32, frame *util.BLL) error {
	if l := frame.ByteLength; l < 4 {
		aac.Error("AVCC data too short", zap.Int("len", l))
		return io.ErrShortWrite
	}
	if frame.GetByte(1) == 0 {
		aac.WriteSequenceHead(frame.ToBytes())
		frame.Recycle()
	} else {
		au := frame.ToBuffers()
		au[0] = au[0][2:]
		aac.AppendAuBytes(au...)
		aac.Audio.WriteAVCC(ts, frame)
	}
	return nil
}

func (aac *AAC) CompleteRTP(value *AVFrame) {
	l := value.AUList.ByteLength
	//AU_HEADER_LENGTH,因为单位是bit, 除以8就是auHeader的字节长度；又因为单个auheader字节长度2字节，所以再除以2就是auheader的个数。
	auHeaderLen := []byte{0x00, 0x10, (byte)((l & 0x1fe0) >> 5), (byte)((l & 0x1f) << 3)} // 3 = 16-13, 5 = 8-3
	var packets [][][]byte
	r := value.AUList.Next.Value.NewReader()
	for bufs := r.ReadN(RTPMTU); len(bufs) > 0; bufs = r.ReadN(RTPMTU) {
		packets = append(packets, append(net.Buffers{auHeaderLen}, bufs...))
	}
	aac.PacketizeRTP(packets...)
}
