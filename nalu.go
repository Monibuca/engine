package engine

import (
	"encoding/binary"

	. "github.com/Monibuca/engine/v2/avformat"
)

const (
	fuaHeaderSize       = 2
	stapaHeaderSize     = 1
	stapaNALULengthSize = 2

	naluTypeBitmask   = 0x1F
	naluRefIdcBitmask = 0x60
	fuaStartBitmask   = 0x80 //1000 0000
	fuaEndBitmask     = 0x40 //0100 0000
)

type NALU struct {
	Publisher
	fuBuffer []byte //用于fua的解析暂存的缓存
	lastTs   uint32
	buffer   []byte //用于存储分帧的Nalu数据
}

func (r *NALU) writePicture(ts uint32, head, payload []byte) {
	if r.VideoTag == nil {
		return
	}
	if payload[1]&fuaStartBitmask != 0 {
		if len(r.buffer) > 0 {
			r.PushVideo(r.lastTs, r.buffer)
			r.buffer = nil
		}
		r.buffer = append(r.buffer, head...)
		r.lastTs = ts
	}
	nl := len(payload)
	r.buffer = append(r.buffer, byte(nl>>24), byte(nl>>16), byte(nl>>8), byte(nl))
	r.buffer = append(r.buffer, payload...)
}
func (r *NALU) WriteNALU(ts uint32, payload []byte) {
	nalType := payload[0] & naluTypeBitmask
	switch nalType {
	case NALU_STAPA:
		for currOffset, naluSize := stapaHeaderSize, 0; currOffset < len(payload); currOffset += naluSize {
			naluSize = int(binary.BigEndian.Uint16(payload[currOffset:]))
			currOffset += stapaNALULengthSize
			if currOffset+len(payload) < currOffset+naluSize {
				Printf("STAP-A declared size(%d) is larger then buffer(%d)", naluSize, len(payload)-currOffset)
				return
			}
			r.WriteNALU(ts, payload[currOffset:currOffset+naluSize])
		}
	case NALU_FUA:
		if len(payload) < fuaHeaderSize {
			Printf("Payload is not large enough to be FU-A")
			return
		}
		if payload[1]&fuaStartBitmask != 0 {
			naluRefIdc := payload[0] & naluRefIdcBitmask
			fragmentedNaluType := payload[1] & naluTypeBitmask
			r.fuBuffer = append([]byte{}, payload...)
			r.fuBuffer[fuaHeaderSize-1] = naluRefIdc | fragmentedNaluType
		} else {
			r.fuBuffer = append(r.fuBuffer, payload[fuaHeaderSize:]...)
		}
		if payload[1]&fuaEndBitmask != 0 {
			r.WriteNALU(ts, r.fuBuffer[fuaHeaderSize-1:])
		}
	case NALU_SPS:
		r.WriteSPS(payload)
	case NALU_PPS:
		r.WritePPS(payload)
	case NALU_Access_Unit_Delimiter:

	case NALU_IDR_Picture:
		r.writePicture(ts, RTMP_KEYFRAME_HEAD, payload)
	case NALU_Non_IDR_Picture:
		r.writePicture(ts, RTMP_NORMALFRAME_HEAD, payload)
	case NALU_SEI:
	default:
		Printf("nalType not support yet:%d", nalType)
	}
}
