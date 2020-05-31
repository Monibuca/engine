package engine

import (
	"bytes"
	"encoding/binary"
	. "github.com/Monibuca/engine/v2/avformat"
	"github.com/Monibuca/engine/v2/pool"
	"github.com/Monibuca/engine/v2/util"
)

const (
	fuaHeaderSize       = 2
	stapaHeaderSize     = 1
	stapaNALULengthSize = 2

	naluTypeBitmask   = 0x1F
	naluRefIdcBitmask = 0x60
	fuaStartBitmask   = 0x80
	fuaEndBitmask     = 0x40
)

type NALU struct {
	Publisher
	fuBuffer []byte //用于fua的解析暂存的缓存
}

func (r *NALU) WriteNALU(ts uint32, payload []byte) {
	nalType := payload[0] & naluTypeBitmask
	buffer := r.AVRing.Buffer
	if buffer == nil {
		buffer = bytes.NewBuffer([]byte{})
		r.AVRing.Buffer = buffer
	}
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
		r.PushVideo(ts, r.AVRing.Bytes())
		r.GetBuffer()
	case NALU_IDR_Picture:
		if r.VideoTag == nil {
			break
		}
		if buffer.Len() == 0 {
			buffer.Write(RTMP_KEYFRAME_HEAD)
		}
		nl := pool.GetSlice(4)
		util.BigEndian.PutUint32(nl, uint32(len(payload)))
		buffer.Write(nl)
		pool.RecycleSlice(nl)
		buffer.Write(payload)
	case NALU_Non_IDR_Picture:
		if r.VideoTag == nil {
			break
		}
		if buffer.Len() == 0 {
			buffer.Write(RTMP_NORMALFRAME_HEAD)
		}
		nl := pool.GetSlice(4)
		util.BigEndian.PutUint32(nl, uint32(len(payload)))
		buffer.Write(nl)
		pool.RecycleSlice(nl)
		buffer.Write(payload)
	default:
		Printf("nalType not support yet:%d", nalType)
	}
}
