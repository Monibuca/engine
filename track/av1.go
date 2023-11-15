package track

import (
	"io"

	"github.com/bluenviron/gortsplib/v4/pkg/format/rtpav1"
	"go.uber.org/zap"
	"m7s.live/engine/v4/codec"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/util"
)

var _ SpesificTrack = (*AV1)(nil)

type AV1 struct {
	Video
	decoder rtpav1.Decoder
}

func NewAV1(stream IStream, stuff ...any) (vt *AV1) {
	vt = &AV1{}
	vt.Video.CodecID = codec.CodecID_AV1
	vt.SetStuff("av1", byte(96), uint32(90000), vt, stuff, stream)
	if vt.BytesPool == nil {
		vt.BytesPool = make(util.BytesPool, 17)
	}
	vt.nalulenSize = 4
	vt.dtsEst = NewDTSEstimator()
	return
}

func (vt *AV1) writeSequenceHead(head []byte) (err error) {
	vt.WriteSequenceHead(head)
	var info codec.AV1CodecConfigurationRecord
	info.Unmarshal(head[5:])
	vt.ParamaterSets[0] = info.ConfigOBUs
	return
}

func (vt *AV1) WriteAVCC(ts uint32, frame *util.BLL) (err error) {
	if l := frame.ByteLength; l < 6 {
		vt.Error("AVCC data too short", zap.Int("len", l))
		return io.ErrShortWrite
	}
	b0 := frame.GetByte(0)
	if isExtHeader := (b0 >> 4) & 0b1000; isExtHeader != 0 {
		firstBuffer := frame.Next.Value
		packetType := b0 & 0b1111
		switch packetType {
		case codec.PacketTypeSequenceStart:
			header := frame.ToBytes()
			header[0] = 0x1d
			header[1] = 0x00
			header[2] = 0x00
			header[3] = 0x00
			header[4] = 0x00
			err = vt.writeSequenceHead(header)
			frame.Recycle()
			return
		case codec.PacketTypeCodedFrames:
			firstBuffer[0] = b0 & 0b0111_1111 & 0xFD
			firstBuffer[1] = 0x01
			copy(firstBuffer[2:], firstBuffer[5:])
			frame.Next.Value = firstBuffer[:firstBuffer.Len()-3]
			frame.ByteLength -= 3
			return vt.Video.WriteAVCC(ts, frame)
		case codec.PacketTypeCodedFramesX:
			firstBuffer[0] = b0 & 0b0111_1111 & 0xFD
			firstBuffer[1] = 0x01
			firstBuffer[2] = 0
			firstBuffer[3] = 0
			firstBuffer[4] = 0
			return vt.Video.WriteAVCC(ts, frame)
		}
	} else {
		if frame.GetByte(1) == 0 {
			err = vt.writeSequenceHead(frame.ToBytes())
			frame.Recycle()
			return
		} else {
			return vt.Video.WriteAVCC(ts, frame)
		}
	}
	return
}

func (vt *AV1) WriteRTPFrame(rtpItem *util.ListItem[RTPFrame]) {
	defer func() {
		err := recover()
		if err != nil {
			vt.Error("WriteRTPFrame panic", zap.Any("err", err))
			vt.Stream.Close()
		}
	}()
	if vt.lastSeq != vt.lastSeq2+1 && vt.lastSeq2 != 0 {
		vt.lostFlag = true
		vt.Warn("lost rtp packet", zap.Uint16("lastSeq", vt.lastSeq), zap.Uint16("lastSeq2", vt.lastSeq2))
	}
	frame := &rtpItem.Value
	rv := vt.Value
	rv.RTP.Push(rtpItem)
	obus, err := vt.decoder.Decode(frame.Packet)
	for _, obu := range obus {
		rv.AUList.Push(vt.BytesPool.GetShell(obu))
	}
	if err == nil {
		vt.generateTimestamp(frame.Timestamp)
		vt.Flush()
	}
}
