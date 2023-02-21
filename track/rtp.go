package track

import (
	"github.com/pion/rtp"
	"go.uber.org/zap"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/config"
	"m7s.live/engine/v4/util"
)

const RTPMTU = 1400

func (av *Media) UnmarshalRTPPacket(p *rtp.Packet) (frame *RTPFrame) {
	if av.PayloadType != p.PayloadType {
		av.Warn("RTP PayloadType error", zap.Uint8("want", av.PayloadType), zap.Uint8("got", p.PayloadType))
		return
	}
	frame = &RTPFrame{
		Packet: *p,
	}
	av.Value.BytesIn += len(p.Payload) + 12
	return av.recorderRTP(frame)
}

func (av *Media) UnmarshalRTP(raw []byte) (frame *RTPFrame) {
	var p rtp.Packet
	err := p.Unmarshal(raw)
	if err != nil {
		av.Warn("RTP Unmarshal error", zap.Error(err))
		return
	}
	return av.UnmarshalRTPPacket(&p)
}

func (av *Media) writeRTPFrame(frame *RTPFrame) {
	if len(frame.Payload) == 0 {
		return
	}
	av.Value.RTP.PushValue(*frame)
	av.WriteRTPFrame(frame)
}

// WriteRTPPack 写入已反序列化的RTP包
func (av *Media) WriteRTPPack(p *rtp.Packet) {
	for frame := av.UnmarshalRTPPacket(p); frame != nil; frame = av.nextRTPFrame() {
		av.writeRTPFrame(frame)
	}
}

// WriteRTP 写入未反序列化的RTP包
func (av *Media) WriteRTP(raw []byte) {
	for frame := av.UnmarshalRTP(raw); frame != nil; frame = av.nextRTPFrame() {
		av.writeRTPFrame(frame)
	}
}

// https://www.cnblogs.com/moonwalk/p/15903760.html
// Packetize packetizes the payload of an RTP packet and returns one or more RTP packets
func (av *Media) PacketizeRTP(payloads ...[][]byte) {
	packetCount := len(payloads)
	for i, pp := range payloads {
		av.rtpSequence++
		rtpItem := av.rtpPool.Get()
		packet := &rtpItem.Value
		if packet.Payload == nil {
			packet.Payload = make([]byte, 0, RTPMTU)
			packet.Version = 2
			packet.PayloadType = av.PayloadType
			packet.SSRC = av.SSRC
		}
		packet.Payload = packet.Payload[:0]
		packet.SequenceNumber = av.rtpSequence
		if av.SampleRate != 90000 {
			packet.Timestamp = uint32(uint64(av.SampleRate) * uint64(av.Value.PTS) / 90000)
		} else {
			packet.Timestamp = av.Value.PTS
		}
		packet.Marker = i == packetCount-1
		for _, p := range pp {
			packet.Payload = append(packet.Payload, p...)
		}
		av.Value.RTP.Push(rtpItem)
	}
}

type RTPDemuxer struct {
	lastSeq  uint16 //上一个收到的序号，用于乱序重排
	lastSeq2 uint16 //记录上上一个收到的序列号
	乱序重排     util.RTPReorder[*RTPFrame]
}

// 获取缓存中下一个rtpFrame
func (av *RTPDemuxer) nextRTPFrame() (frame *RTPFrame) {
	if config.Global.RTPReorder {
		return av.乱序重排.Pop()
	}
	return
}

// 对RTP包乱序重排
func (av *RTPDemuxer) recorderRTP(frame *RTPFrame) *RTPFrame {
	if config.Global.RTPReorder {
		return av.乱序重排.Push(frame.SequenceNumber, frame)
	} else {
		if av.lastSeq == 0 {
			av.lastSeq = frame.SequenceNumber
		} else if frame.SequenceNumber == av.lastSeq2+1 { // 本次序号是上上次的序号+1 说明中间隔了一个错误序号（某些rtsp流中的rtcp包写成了rtp包导致的）
			av.lastSeq = frame.SequenceNumber
		} else {
			av.lastSeq2 = av.lastSeq
			av.lastSeq = frame.SequenceNumber
			if av.lastSeq != av.lastSeq2+1 { //序号不连续
				// av.Stream.Warn("RTP SequenceNumber error", av.lastSeq2, av.lastSeq)
				return frame
			}
		}
		return frame
	}
}

type RTPMuxer struct {
	rtpSequence uint16 //用于生成下一个rtp包的序号
}
