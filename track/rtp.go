package track

import (
	"github.com/pion/rtp"
	"go.uber.org/zap"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/util"
)

const RTPMTU = 1400

// WriteRTPPack 写入已反序列化的RTP包，已经排序过了的
func (av *Media) WriteRTPPack(p *rtp.Packet) {
	var frame RTPFrame
	frame.Packet = p
	av.Value.BytesIn += len(frame.Payload) + 12
	av.Value.RTP.PushValue(frame)
	if len(p.Payload) > 0 {
		av.WriteRTPFrame(&frame)
	}
}

// WriteRTPFrame 写入未反序列化的RTP包, 未排序的
func (av *Media) WriteRTP(raw *util.ListItem[RTPFrame]) {
	for frame := av.recorderRTP(raw); frame != nil; frame = av.nextRTPFrame() {
		av.Value.BytesIn += len(frame.Value.Payload) + 12
		if len(frame.Value.Payload) > 0 {
			av.Value.RTP.Push(frame)
			av.WriteRTPFrame(&frame.Value)
			// av.Info("rtp", zap.Uint32("ts", (frame.Value.Timestamp)), zap.Int("len", len(frame.Value.Payload)), zap.Bool("marker", frame.Value.Marker), zap.Uint16("seq", frame.Value.SequenceNumber))
		} else {
			av.Warn("rtp payload is empty", zap.Uint32("ts", (frame.Value.Timestamp)), zap.Any("ext", frame.Value.GetExtensionIDs()), zap.Uint16("seq", frame.Value.SequenceNumber))
			frame.Recycle()
		}
	}
}

// https://www.cnblogs.com/moonwalk/p/15903760.html
// Packetize packetizes the payload of an RTP packet and returns one or more RTP packets
func (av *Media) PacketizeRTP(payloads ...[][]byte) {
	packetCount := len(payloads)
	for i, pp := range payloads {
		av.rtpSequence++
		rtpItem := av.GetRTPFromPool()
		packet := &rtpItem.Value
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
	lastSeq  uint16 //上一个rtp包的序号
	lastSeq2 uint16 //上上一个rtp包的序号
	乱序重排     util.RTPReorder[*util.ListItem[RTPFrame]]
}

// 获取缓存中下一个rtpFrame
func (av *RTPDemuxer) nextRTPFrame() (frame *util.ListItem[RTPFrame]) {
	frame = av.乱序重排.Pop()
	if frame == nil {
		return
	}
	av.lastSeq2 = av.lastSeq
	av.lastSeq = frame.Value.SequenceNumber
	return
}

// 对RTP包乱序重排
func (av *RTPDemuxer) recorderRTP(item *util.ListItem[RTPFrame]) (frame *util.ListItem[RTPFrame]) {
	frame = av.乱序重排.Push(item.Value.SequenceNumber, item)
	if frame == nil {
		return
	}
	av.lastSeq2 = av.lastSeq
	av.lastSeq = frame.Value.SequenceNumber
	return
}

type RTPMuxer struct {
	rtpSequence uint16 //用于生成下一个rtp包的序号
}
