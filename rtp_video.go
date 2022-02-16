package engine

import (
	"encoding/binary"

	"github.com/Monibuca/utils/v3"
	"github.com/Monibuca/utils/v3/codec"
	// "github.com/pion/rtp/codecs"
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
	RTPDemuxer `json:"-"`
	*VideoTrack
	fuaPayload []byte
	demuxNalu  func([]byte) *RTPNalu
}

func (s *Stream) NewRTPVideo(codecID byte) (r *RTPVideo) {
	r = &RTPVideo{
		VideoTrack: s.NewVideoTrack(codecID),
	}
	if config.RTPReorder {
		r.orderMap = make(map[uint16]RTPNalu)
	}
	r.timeBase = &r.timebase
	switch codecID {
	case codec.CodecID_H264:
		r.demuxNalu = r.demuxH264
	case codec.CodecID_H265:
		r.demuxNalu = r.demuxH265
	}
	r.OnDemux = r._demux
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
			*current = &RTPNalu{Payload: payload[currOffset : currOffset+naluSize], PTS: v.PTS}
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
			*current = &RTPNalu{Payload: payload[currOffset : currOffset+naluSize], PTS: v.PTS + uint32(ts)}
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
			v.fuaPayload = make([]byte, 1)
			v.fuaPayload[0] = (payload[0] & naluRefIdcBitmask) | (payload[1] & naluTypeBitmask)
		}
		if v.fuaPayload != nil {
			if v.fuaPayload = append(v.fuaPayload, payload[lenSize:]...); payload[1]&fuaEndBitmask != 0 {
				result = &RTPNalu{Payload: v.fuaPayload, PTS: v.PTS}
				v.fuaPayload = nil
			}
		}
	default:
		return &RTPNalu{Payload: payload, PTS: v.PTS}
	}
	return
}

func (v *RTPVideo) demuxH265(payload []byte) (result *RTPNalu) {
	naluLen := len(payload)
	if naluLen == 0 {
		return
	}
	naluType := payload[0] & naluTypeBitmask_hevc >> 1
	switch naluType {
	// 4.4.2. Aggregation Packets (APs) (p25)
	/*
	    0               1               2               3
	    0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
	   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	   |      PayloadHdr (Type=48)     |           NALU 1 DONL         |
	   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	   |           NALU 1 Size         |            NALU 1 HDR         |
	   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	   |                                                               |
	   |                         NALU 1 Data . . .                     |
	   |                                                               |
	   +     . . .     +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	   |               |  NALU 2 DOND  |            NALU 2 Size        |
	   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	   |          NALU 2 HDR           |                               |
	   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+            NALU 2 Data        |
	   |                                                               |
	   |         . . .                 +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	   |                               :    ...OPTIONAL RTP padding    |
	   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	*/
	case codec.NAL_UNIT_UNSPECIFIED_48:
		currOffset := 2
		if v.UsingDonlField {
			currOffset = 4
		}
		current := &result
		for naluSize := 0; currOffset < naluLen; currOffset += naluSize {
			naluSize = int(binary.BigEndian.Uint16(payload[currOffset:]))
			currOffset += 2
			if naluLen < currOffset+naluSize {
				utils.Printf("STAP-A declared size(%d) is larger then buffer(%d)", naluSize, naluLen-currOffset)
				return
			}
			*current = &RTPNalu{Payload: payload[currOffset : currOffset+naluSize], PTS: v.PTS}
			current = &(*current).Next
			if v.UsingDonlField {
				currOffset += 1
			}
		}
		// 4.4.3. Fragmentation Units (p29)
		/*
		    0               1               2               3
		    0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
		   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
		   |     PayloadHdr (Type=49)      |    FU header  |  DONL (cond)  |
		   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-|
		   |  DONL (cond)  |                                               |
		   |-+-+-+-+-+-+-+-+                                               |
		   |                           FU payload                          |
		   |                                                               |
		   |                               +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
		   |                               :    ...OPTIONAL RTP padding    |
		   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
		   +---------------+
		   |0|1|2|3|4|5|6|7|
		   +-+-+-+-+-+-+-+-+
		   |S|E|   FuType  |
		   +---------------+
		*/
	case codec.NAL_UNIT_UNSPECIFIED_49:
		offset := 3
		if v.UsingDonlField {
			offset = 5
		}
		if naluLen < offset {
			return
		}
		fuheader := payload[2]
		if naluType = fuheader & 0b00111111; fuheader&fuaStartBitmask != 0 {
			v.fuaPayload = make([]byte, 2)
			payload[0] = payload[0]&0b10000001 | (naluType << 1)
			copy(v.fuaPayload, payload[:2])
		}
		if v.fuaPayload != nil {
			if v.fuaPayload = append(v.fuaPayload, payload[offset:]...); payload[1]&fuaEndBitmask != 0 {
				result = &RTPNalu{Payload: v.fuaPayload, PTS: v.PTS}
				v.fuaPayload = nil
			}
		}
	default:
		return &RTPNalu{Payload: payload, PTS: v.PTS}
	}
	return
}

// func (p *RTPVideo) demuxH265(payload []byte) (result *RTPNalu) {
// 	var h265 codecs.H265Packet
// 	if _, err := h265.Unmarshal(payload); err == nil {
// 		switch v := h265.Packet().(type) {
// 		case (*codecs.H265FragmentationUnitPacket):
// 			if v.FuHeader().S() {
// 				p.fuaBuffer = bytes.NewBuffer([]byte{})
// 				payload[0] = payload[0]&0b10000001 | ((byte(v.FuHeader()) & 0b00111111) << 1)
// 				p.fuaBuffer.Write(payload[:2])
// 			}
// 			p.fuaBuffer.Write(v.Payload())
// 			if v.FuHeader().E() {
// 				result = &RTPNalu{Payload: p.fuaBuffer.Bytes(), PTS: p.Timestamp}
// 				p.fuaBuffer = nil
// 			}
// 		case (*codecs.H265AggregationPacket):
// 			head := &RTPNalu{Payload: v.FirstUnit().NalUnit(), PTS: p.Timestamp}
// 			for _, nalu := range v.OtherUnits() {
// 				head.Next = &RTPNalu{Payload: nalu.NalUnit(), PTS: p.Timestamp}
// 				head = head.Next
// 			}
// 			return head
// 		case (*codecs.H265PACIPacket):
// 			return &RTPNalu{Payload: v.Payload(), PTS: p.Timestamp}
// 		case (*codecs.H265SingleNALUnitPacket):
// 			return &RTPNalu{Payload: v.Payload(), PTS: p.Timestamp}
// 		}
// 	}
// 	return
// }

// func (p *RTPVideo) _demux(ts uint32, payload []byte) {
// 	p.timestamp = time.Now()
// 	if last := p.demuxNalu(payload); last != nil {
// 		p.OnDemux = func(ts uint32, payload []byte) {
// 			if current := p.demuxNalu(payload); current != nil {
// 				if last.PTS > current.PTS { //有B帧
// 					var b B
// 					utils.Println("rtp has B-frame!!")
// 					for heap.Push(&b, last); last.Next != nil; last = last.Next {
// 						heap.Push(&b, last.Next)
// 					}
// 					for heap.Push(&b, current); current.Next != nil; current = current.Next {
// 						heap.Push(&b, current.Next)
// 					}
// 					p.OnDemux = func(ts uint32, payload []byte) {
// 						if current := p.demuxNalu(payload); current != nil {
// 							if current.PTS > b.MaxTS {
// 								for b.Len() > 0 {
// 									el := heap.Pop(&b).(struct {
// 										DTS uint32
// 										*RTPNalu
// 									})
// 									p.PushNalu(el.DTS, (el.PTS - el.DTS), el.Payload)
// 								}
// 								b.MaxTS = 0
// 							}
// 							for heap.Push(&b, current); current.Next != nil; current = current.Next {
// 								heap.Push(&b, current.Next)
// 							}
// 						}
// 					}
// 					return
// 				}
// 				p.PushNalu(p.PTS, 0, last.Payload)
// 				for last = current; last.Next != nil; last = last.Next {
// 					p.PushNalu(p.PTS, 0, last.Payload)
// 				}
// 			}
// 		}
// 	}
// }

func (p *RTPVideo) _demux(ts uint32, payload []byte) {
	if nalus := p.demuxNalu(payload); nalus != nil {
		startPTS := nalus.PTS
		dtsEst := NewDTSEstimator()
		dts := dtsEst.Feed(0)
		p.PushNalu(dts, 0, nalus.Payload)
		var cache [][]byte
		pts := startPTS
		for nalus = nalus.Next; nalus != nil; nalus = nalus.Next {
			pts = nalus.PTS - startPTS
			dts = dtsEst.Feed(pts)
			p.PushNalu(dts, pts-dts, nalus.Payload)
		}
		p.OnDemux = func(ts uint32, payload []byte) {
			for nalus := p.demuxNalu(p.Payload); nalus != nil; nalus = nalus.Next {
				if len(cache) == 0 {
					pts = nalus.PTS - startPTS
					dts = dtsEst.Feed(pts)
				}
				cache = append(cache, nalus.Payload)
				if p.Marker && nalus.Next == nil {
					p.PushNalu(dts, pts-dts, cache...)
					cache = cache[:0]
				}
			}
		}
	}
}
