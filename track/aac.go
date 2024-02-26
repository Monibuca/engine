package track

import (
	"fmt"
	"io"
	"net"

	"github.com/bluenviron/mediacommon/pkg/bits"
	"go.uber.org/zap"
	"m7s.live/engine/v4/codec"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/util"
)

var _ SpesificTrack = (*AAC)(nil)

func NewAAC(puber IPuber, stuff ...any) (aac *AAC) {
	aac = &AAC{
		Mode: 2,
	}
	aac.SizeLength = 13
	aac.IndexLength = 3
	aac.IndexDeltaLength = 3
	aac.CodecID = codec.CodecID_AAC
	aac.Channels = 2
	aac.SampleSize = 16
	aac.SetStuff("aac", byte(97), aac, stuff, puber)
	if aac.BytesPool == nil {
		aac.BytesPool = make(util.BytesPool, 17)
	}
	aac.AVCCHead = []byte{0xAF, 1}
	return
}

type AAC struct {
	Audio

	Mode      int       // 1为lbr，2为hbr
	fragments *util.BLL // 用于处理不完整的AU,缺少的字节数
}

func (aac *AAC) WriteADTS(ts uint32, b util.IBytes) {
	adts := b.Bytes()
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
	aac.Value.ADTS = aac.GetFromPool(b)
	aac.Flush()
}

// https://datatracker.ietf.org/doc/html/rfc3640#section-3.2.1
func (aac *AAC) WriteRTPFrame(rtpItem *util.ListItem[RTPFrame]) {
	aac.Value.RTP.Push(rtpItem)
	frame := &rtpItem.Value
	if len(frame.Payload) < 2 {
		// aac.fragments = aac.fragments[:0]
		return
	}
	if aac.SampleRate != 90000 {
		aac.generateTimestamp(uint32(uint64(frame.Timestamp) * 90000 / uint64(aac.SampleRate)))
	}
	auHeaderLen := util.ReadBE[int](frame.Payload[:2]) //通常为16，即一个AU Header的长度
	if auHeaderLen == 0 {
		aac.Value.AUList.Push(aac.BytesPool.GetShell(frame.Payload[:2]))
		aac.Flush()
	} else {
		payload := frame.Payload[2:]
		// AU-headers
		dataLens, err := aac.readAUHeaders(payload, auHeaderLen)
		if err != nil {
			// discard pending fragmented packets
			return
		}

		pos := (auHeaderLen >> 3)
		if (auHeaderLen % 8) != 0 {
			pos++
		}
		payload = payload[pos:]

		if aac.fragments == nil {
			if frame.Header.Marker {
				// AUs
				for _, dataLen := range dataLens {
					if len(payload) < int(dataLen) {
						aac.fragments = &util.BLL{}
						aac.fragments.Push(aac.BytesPool.GetShell(payload))
						// aac.fragments = aac.fragments[:0]
						// aac.Error("payload is too short 1", zap.Int("dataLen", int(dataLen)), zap.Int("len", len(payload)))
						return
					}
					aac.AppendAuBytes(payload[:dataLen])
					payload = payload[dataLen:]
				}
			} else {
				if len(dataLens) != 1 {
					// aac.fragments = aac.fragments[:0]
					aac.Error("a fragmented packet can only contain one AU")
					return
				}
				aac.fragments = &util.BLL{}
				// if len(payload) < int(dataLens[0]) {
				// 	aac.fragments = aac.fragments[:0]
				// 	aac.Error("payload is too short 2", zap.Int("dataLen", int(dataLens[0])), zap.Int("len", len(payload)))
				// 	return
				// }
				aac.fragments.Push(aac.BytesPool.GetShell(payload))
				// aac.fragments = append(aac.fragments, payload[:dataLens[0]])
				return
			}
		} else {
			// we are decoding a fragmented AU
			if len(dataLens) != 1 {
				aac.fragments.Recycle()
				aac.fragments = nil
				// aac.fragments = aac.fragments[:0]
				aac.Error("a fragmented packet can only contain one AU")
				return
			}

			// if len(payload) < int(dataLens[0]) {
			// 	aac.fragments = aac.fragments[:0]
			// 	aac.Error("payload is too short 3", zap.Int("dataLen", int(dataLens[0])), zap.Int("len", len(payload)))
			// 	return
			// }

			// if fragmentedSize := util.SizeOfBuffers(aac.fragments) + int(dataLens[0]); fragmentedSize > 5*1024 {
			// 	aac.fragments = aac.fragments[:0] // discard pending fragmented packets
			// 	aac.Error(fmt.Sprintf("AU size (%d) is too big (maximum is %d)", fragmentedSize, 5*1024))
			// 	return
			// }

			// aac.fragments = append(aac.fragments, payload[:dataLens[0]])
			aac.fragments.Push(aac.BytesPool.GetShell(payload))
			if !frame.Header.Marker {
				return
			}
			if uint64(aac.fragments.ByteLength) != dataLens[0] {
				aac.Error("fragmented AU size is not correct", zap.Uint64("dataLen", dataLens[0]), zap.Int("len", aac.fragments.ByteLength))
			}
			aac.Value.AUList.PushValue(aac.fragments)
			// aac.AppendAuBytes(aac.fragments...)

			aac.fragments = nil
		}
		aac.Flush()
	}
}

func (aac *AAC) WriteSequenceHead(sh []byte) error {
	aac.Media.WriteSequenceHead(sh)
	config1, config2 := aac.SequenceHead[2], aac.SequenceHead[3]
	aac.Channels = ((config2 >> 3) & 0x0F) //声道
	aac.SampleRate = uint32(codec.SamplingFrequencies[((config1&0x7)<<1)|(config2>>7)])
	aac.Parse(aac.SequenceHead[2:])
	go aac.Attach()
	return nil
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

func (aac *AAC) readAUHeaders(buf []byte, headersLen int) ([]uint64, error) {
	firstRead := false

	count := 0
	for i := 0; i < headersLen; {
		if i == 0 {
			i += aac.SizeLength
			i += aac.IndexLength
		} else {
			i += aac.SizeLength
			i += aac.IndexDeltaLength
		}
		count++
	}

	dataLens := make([]uint64, count)

	pos := 0
	i := 0

	for headersLen > 0 {
		dataLen, err := bits.ReadBits(buf, &pos, aac.SizeLength)
		if err != nil {
			return nil, err
		}
		headersLen -= aac.SizeLength

		if !firstRead {
			firstRead = true
			if aac.IndexLength > 0 {
				auIndex, err := bits.ReadBits(buf, &pos, aac.IndexLength)
				if err != nil {
					return nil, err
				}
				headersLen -= aac.IndexLength

				if auIndex != 0 {
					return nil, fmt.Errorf("AU-index different than zero is not supported")
				}
			}
		} else if aac.IndexDeltaLength > 0 {
			auIndexDelta, err := bits.ReadBits(buf, &pos, aac.IndexDeltaLength)
			if err != nil {
				return nil, err
			}
			headersLen -= aac.IndexDeltaLength

			if auIndexDelta != 0 {
				return nil, fmt.Errorf("AU-index-delta different than zero is not supported")
			}
		}

		dataLens[i] = dataLen
		i++
	}

	return dataLens, nil
}
