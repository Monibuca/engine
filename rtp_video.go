package engine

import (
	"bytes"
	"container/heap"
	"encoding/binary"

	"github.com/Monibuca/utils/v3"
	"github.com/Monibuca/utils/v3/codec"
	"github.com/pion/rtp/codecs"
)

const (
	fuaStartBitmask     = 0b1000_0000
	fuaEndBitmask       = 0b0100_0000
	stapaNALULengthSize = 2
	naluRefIdcBitmask   = 0x60
)

var sizeMap = map[uint8]int{
	codec.NALU_STAPA:  1,
	codec.NALU_STAPB:  3,
	codec.NALU_MTAP16: 4,
	codec.NALU_MTAP24: 5,
	codec.NALU_FUA:    2,
	codec.NALU_FUB:    4,
}

type RTPNalu struct {
	Payload []byte
	PTS     uint32
	Next    *RTPNalu
}

type RTPVideo struct {
	RTPPublisher
	*VideoTrack
	fuaBuffer *bytes.Buffer
	demuxNalu func([]byte) *RTPNalu
}

func (s *Stream) NewRTPVideo(codecID byte) (r *RTPVideo) {
	r = &RTPVideo{
		VideoTrack: s.NewVideoTrack(codecID),
	}
	switch codecID {
	case codec.CodecID_H264:
		r.demuxNalu = r.demuxH264
	case codec.CodecID_H265:
		r.demuxNalu = r.demuxH265
	}
	r.demux = r._demux
	return
}

func (v *RTPVideo) demuxH264(payload []byte) (result *RTPNalu) {
	naluLen := len(payload)
	if naluLen == 0 {
		return
	}
	naluType := payload[0] & naluTypeBitmask
	lenSize := sizeMap[naluType]
	switch naluType {
	case codec.NALU_STAPA, codec.NALU_STAPB:
		current := &result
		for currOffset, naluSize := lenSize, 0; currOffset < naluLen; currOffset += naluSize {
			naluSize = int(binary.BigEndian.Uint16(payload[currOffset:]))
			if currOffset += stapaNALULengthSize; naluLen < currOffset+naluSize {
				utils.Printf("STAP-A declared size(%d) is larger then buffer(%d)", naluSize, naluLen-currOffset)
				return
			}
			*current = &RTPNalu{Payload: payload[currOffset : currOffset+naluSize], PTS: v.Timestamp}
			current = &(*current).Next
		}
	case codec.NALU_MTAP16, codec.NALU_MTAP24:
		current := &result
		for currOffset, naluSize := 3, 0; currOffset < naluLen; currOffset += naluSize {
			naluSize = int(binary.BigEndian.Uint16(payload[currOffset:]))
			currOffset += lenSize
			if naluLen < currOffset+naluSize {
				utils.Printf("MTAP16 declared size(%d) is larger then buffer(%d)", naluSize, naluLen-currOffset)
				return
			}
			ts := binary.BigEndian.Uint16(payload[currOffset+3:])
			if lenSize == 5 {
				ts = (ts << 8) | uint16(payload[currOffset+5])
			}
			*current = &RTPNalu{Payload: payload[currOffset : currOffset+naluSize], PTS: v.Timestamp + uint32(ts)}
			current = &(*current).Next
		}
		/*
			0                   1                   2                   3
			0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
			+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
			|   PayloadHdr (Type=29)        |   FU header   | DONL (cond)   |
			+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-|
			|   DONL (cond) |                                               |
			|-+-+-+-+-+-+-+-+                                               |
			|                         FU payload                            |
			|                                                               |
			|                               +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
			|                               :...OPTIONAL RTP padding        |
			+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
		*/
		/*
			0                   1                   2                   3
			0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
			+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
			|   PayloadHdr (Type=28)        |         NALU 1 Size           |
			+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
			|          NALU 1 HDR           |                               |
			+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+         NALU 1 Data           |
			|                   . . .                                       |
			|                                                               |
			+               +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
			|  . . .        | NALU 2 Size                   | NALU 2 HDR    |
			+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
			| NALU 2 HDR    |                                               |
			+-+-+-+-+-+-+-+-+              NALU 2 Data                      |
			|                   . . .                                       |
			|                               +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
			|                               :...OPTIONAL RTP padding        |
			+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
		*/
	case codec.NALU_FUA, codec.NALU_FUB:
		if naluLen < lenSize {
			utils.Printf("Payload is not large enough to be FU-A")
			return
		}
		if payload[1]&fuaStartBitmask != 0 {
			v.fuaBuffer = bytes.NewBuffer([]byte{})
			v.fuaBuffer.WriteByte((payload[0] & naluRefIdcBitmask) | (payload[1] & naluTypeBitmask))
		}
		if v.fuaBuffer != nil {
			if v.fuaBuffer.Write(payload[lenSize:]); payload[1]&fuaEndBitmask != 0 {
				result = &RTPNalu{Payload: v.fuaBuffer.Bytes(), PTS: v.Timestamp}
				v.fuaBuffer = nil
			}
		}
	default:
		return &RTPNalu{Payload: payload, PTS: v.Timestamp}
	}
	return
}

// func (v *RTPVideo) demuxH265(payload []byte) (result *RTPNalu) {
// 	naluLen := len(payload)
// 	if naluLen == 0 {
// 		return
// 	}
// 	naluType := payload[0] & naluTypeBitmask_hevc >> 1
// 	switch naluType {
// 	// 4.4.2. Aggregation Packets (APs) (p25)
// 	/*
// 	    0               1               2               3
// 	    0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
// 	   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// 	   |      PayloadHdr (Type=48)     |           NALU 1 DONL         |
// 	   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// 	   |           NALU 1 Size         |            NALU 1 HDR         |
// 	   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// 	   |                                                               |
// 	   |                         NALU 1 Data . . .                     |
// 	   |                                                               |
// 	   +     . . .     +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// 	   |               |  NALU 2 DOND  |            NALU 2 Size        |
// 	   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// 	   |          NALU 2 HDR           |                               |
// 	   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+            NALU 2 Data        |
// 	   |                                                               |
// 	   |         . . .                 +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// 	   |                               :    ...OPTIONAL RTP padding    |
// 	   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// 	*/
// 	case codec.NAL_UNIT_UNSPECIFIED_48:
// 		currOffset := 2
// 		if v.UsingDonlField {
// 			currOffset = 4
// 		}
// 		current := &result
// 		for naluSize := 0; currOffset < naluLen; currOffset += naluSize {
// 			naluSize = int(binary.BigEndian.Uint16(payload[currOffset:]))
// 			currOffset += 2
// 			if naluLen < currOffset+naluSize {
// 				utils.Printf("STAP-A declared size(%d) is larger then buffer(%d)", naluSize, naluLen-currOffset)
// 				return
// 			}
// 			*current = &RTPNalu{Payload: payload[currOffset : currOffset+naluSize], Ts: v.absTs}
// 			current = &(*current).Next
// 			if v.UsingDonlField {
// 				currOffset += 1
// 			}
// 		}
// 		// 4.4.3. Fragmentation Units (p29)
// 		/*
// 		    0               1               2               3
// 		    0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
// 		   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// 		   |     PayloadHdr (Type=49)      |    FU header  |  DONL (cond)  |
// 		   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-|
// 		   |  DONL (cond)  |                                               |
// 		   |-+-+-+-+-+-+-+-+                                               |
// 		   |                           FU payload                          |
// 		   |                                                               |
// 		   |                               +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// 		   |                               :    ...OPTIONAL RTP padding    |
// 		   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// 		   +---------------+
// 		   |0|1|2|3|4|5|6|7|
// 		   +-+-+-+-+-+-+-+-+
// 		   |S|E|   FuType  |
// 		   +---------------+
// 		*/
// 	case codec.NAL_UNIT_UNSPECIFIED_49:
// 		offset := 3
// 		if v.UsingDonlField {
// 			offset = 5
// 		}
// 		if naluLen < offset {
// 			return
// 		}
// 		fuheader := payload[2]
// 		if naluType = fuheader & 0b00111111; fuheader&fuaStartBitmask != 0 {
// 			v.fuaBuffer = bytes.NewBuffer([]byte{})
// 			payload[0] = payload[0]&0b10000001 | (naluType << 1)
// 			v.fuaBuffer.Write(payload[:2])
// 		}
// 		if v.fuaBuffer != nil {
// 			if v.fuaBuffer.Write(payload[offset:]); fuheader&fuaEndBitmask != 0 {
// 				result = &RTPNalu{Payload: v.fuaBuffer.Bytes(), Ts: v.absTs}
// 				v.fuaBuffer = nil
// 			}
// 		}
// 	default:
// 		return &RTPNalu{Payload: payload, Ts: v.absTs}
// 	}
// 	return
// }

func (p *RTPVideo) demuxH265(payload []byte) (result *RTPNalu) {
	var h265 codecs.H265Packet
	if _, err := h265.Unmarshal(payload); err == nil {
		switch v := h265.Packet().(type) {
		case (*codecs.H265FragmentationUnitPacket):
			if v.FuHeader().S() {
				p.fuaBuffer = bytes.NewBuffer([]byte{})
			}
			p.fuaBuffer.Write(v.Payload())
			if v.FuHeader().E() {
				result = &RTPNalu{Payload: p.fuaBuffer.Bytes(), PTS: p.Timestamp}
				p.fuaBuffer = nil
			}
		case (*codecs.H265AggregationPacket):
			head := &RTPNalu{Payload: v.FirstUnit().NalUnit(), PTS: p.Timestamp}
			for _, nalu := range v.OtherUnits() {
				head.Next = &RTPNalu{Payload: nalu.NalUnit(), PTS: p.Timestamp}
				head = head.Next
			}
			return head
		case (*codecs.H265PACIPacket):
			return &RTPNalu{Payload: v.Payload(), PTS: p.Timestamp}
		case (*codecs.H265SingleNALUnitPacket):
			return &RTPNalu{Payload: v.Payload(), PTS: p.Timestamp}
		}
	}
	return
}

func (p *RTPVideo) _demux() {
	if last := p.demuxNalu(p.Payload); last != nil {
		p.demux = func() {
			if current := p.demuxNalu(p.Payload); current != nil {
				if last.PTS > current.PTS { //有B帧
					var b B
					utils.Println("rtp has B-frame!!")
					for heap.Push(&b, last); last.Next != nil; last = last.Next {
						heap.Push(&b, last.Next)
					}
					for heap.Push(&b, current); current.Next != nil; current = current.Next {
						heap.Push(&b, current.Next)
					}
					p.demux = func() {
						if current := p.demuxNalu(p.Payload); current != nil {
							if current.PTS > b.MaxTS {
								for b.Len() > 0 {
									el := heap.Pop(&b).(struct {
										DTS uint32
										*RTPNalu
									})
									p.absTs += (el.DTS - p.lastTs)
									p.lastTs = el.DTS
									p.PushNalu(p.absTs/90, (el.PTS/90 - el.DTS/90), el.Payload)
								}
								b.MaxTS = 0
							}
							for heap.Push(&b, current); current.Next != nil; current = current.Next {
								heap.Push(&b, current.Next)
							}
						}
					}
					return
				}
				if p.lastTs != 0 {
					p.absTs += (last.PTS - p.lastTs)
				}
				p.lastTs = last.PTS
				p.PushNalu(p.absTs/90, 0, last.Payload)
				for last = current; last.Next != nil; last = last.Next {
					p.absTs += (last.PTS - p.lastTs)
					p.lastTs = last.PTS
					p.PushNalu(p.absTs/90, 0, last.Payload)
				}
			}
		}
	}
}
func (p *RTPVideo) Push(payload []byte) {
	if p.Unmarshal(payload) == nil {
		if p.lastSeq > 0 && p.SequenceNumber != p.lastSeq+1 {
			println("RTP Publisher: SequenceNumber error", p.lastSeq, p.SequenceNumber)
			return
		}
		p.lastSeq = p.SequenceNumber
		p.demux()
	}
}
