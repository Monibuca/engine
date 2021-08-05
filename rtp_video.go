package engine

import (
	"bytes"
	"encoding/binary"

	"github.com/Monibuca/utils/v3"
	"github.com/Monibuca/utils/v3/codec"
)

type TSSlice []uint32

func (s TSSlice) Len() int           { return len(s) }
func (s TSSlice) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s TSSlice) Less(i, j int) bool { return s[i] < s[j] }

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
	Ts      uint32
	Next    *RTPNalu
}

type RTPVideo struct {
	RTPPublisher
	*VideoTrack
	fuaBuffer *bytes.Buffer
	demuxNalu func([]byte) *RTPNalu
	p         *RTPNalu //B帧前的P帧
}

func (s *Stream) NewRTPVideo(codec byte) (r *RTPVideo) {
	r = &RTPVideo{
		VideoTrack: s.NewVideoTrack(codec),
	}
	switch codec {
	case 7:
		r.demuxNalu = r.demuxH264
	case 12:
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
			*current = &RTPNalu{Payload: payload[currOffset : currOffset+naluSize], Ts: v.absTs}
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
			*current = &RTPNalu{Payload: payload[currOffset : currOffset+naluSize], Ts: v.absTs + uint32(ts)}
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
				result = &RTPNalu{Payload: v.fuaBuffer.Bytes(), Ts: v.absTs}
				v.fuaBuffer = nil
			}
		}
	default:
		return &RTPNalu{Payload: payload, Ts: v.absTs}
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
			*current = &RTPNalu{Payload: payload[currOffset : currOffset+naluSize], Ts: v.absTs}
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
			v.fuaBuffer = bytes.NewBuffer([]byte{})
			payload[0] = payload[0]&0b10000001 | (naluType << 1)
			v.fuaBuffer.Write(payload[:2])
		}
		if v.fuaBuffer != nil {
			if v.fuaBuffer.Write(payload[offset:]); fuheader&fuaEndBitmask != 0 {
				result = &RTPNalu{Payload: v.fuaBuffer.Bytes(), Ts: v.absTs}
				v.fuaBuffer = nil
			}
		}
	default:
		return &RTPNalu{Payload: payload, Ts: v.absTs}
	}
	return
}

func (v *RTPVideo) _push(last, current *RTPNalu) {
	lastB := false
	var ts, cts uint32 = last.Ts, 0
	if v.p != nil && v.p.Ts < current.Ts {
		lastB = true
	}
	if last.Ts > current.Ts {
		v.p = last
	}
	if v.p != nil {
		if lastB {
			ts = v.p.Ts
			v.p = nil
		} else {
			ts = current.Ts
		}
		cts = (last.Ts - ts) / 90
	}
	v.PushNalu(ts/90, cts, last.Payload)
}

func (v *RTPVideo) _demux() {
	last := v.demuxNalu(v.Payload)
	if last == nil {
		return
	}
	for ; last.Next != nil; last = last.Next {
		v._push(last, last.Next)
	}
	v.demux = func() {
		current := v.demuxNalu(v.Payload)
		if current == nil {
			return
		}
		v._push(last, current)
		for last = current; last.Next != nil; last = last.Next {
			v._push(last, last.Next)
		}
	}
}
