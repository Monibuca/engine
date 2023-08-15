package track

import (
	"io"

	"go.uber.org/zap"
	"m7s.live/engine/v4/codec"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/log"
	"m7s.live/engine/v4/util"
)

var _ SpesificTrack = (*H264)(nil)

type H264 struct {
	Video
}

func NewH264(stream IStream, stuff ...any) (vt *H264) {
	vt = &H264{}
	vt.Video.CodecID = codec.CodecID_H264
	vt.SetStuff("h264", byte(96), uint32(90000), stream, vt)
	vt.SetStuff(stuff...)
	if vt.BytesPool == nil {
		vt.BytesPool = make(util.BytesPool, 17)
	}
	vt.ParamaterSets = make(ParamaterSets, 2)
	vt.nalulenSize = 4
	vt.dtsEst = NewDTSEstimator()
	return
}

func (vt *H264) WriteSliceBytes(slice []byte) {
	naluType := codec.ParseH264NALUType(slice[0])
	if log.Trace {
		vt.Trace("naluType", zap.Uint8("naluType", naluType.Byte()))
	}
	if vt.Value.IFrame {
		vt.AppendAuBytes(slice)
		return
	}
	switch naluType {
	case codec.NALU_SPS:
		spsInfo, _ := codec.ParseSPS(slice)
		if spsInfo.Width != vt.SPSInfo.Width || spsInfo.Height != vt.SPSInfo.Height {
			vt.Debug("SPS", zap.Any("SPSInfo", spsInfo))
		}
		vt.SPSInfo = spsInfo
		vt.Video.SPS = slice
		vt.ParamaterSets[0] = slice
	case codec.NALU_PPS:
		vt.Video.PPS = slice
		vt.ParamaterSets[1] = slice
		lenSPS := len(vt.Video.SPS)
		lenPPS := len(vt.Video.PPS)
		var b util.Buffer
		if lenSPS > 3 {
			b.Write(codec.RTMP_AVC_HEAD[:6])
			b.Write(vt.Video.SPS[1:4])
			b.Write(codec.RTMP_AVC_HEAD[9:10])
		} else {
			b.Write(codec.RTMP_AVC_HEAD)
		}
		b.WriteByte(0xE1)
		b.WriteUint16(uint16(lenSPS))
		b.Write(vt.Video.SPS)
		b.WriteByte(0x01)
		b.WriteUint16(uint16(lenPPS))
		b.Write(vt.Video.PPS)
		vt.WriteSequenceHead(b)
	case codec.NALU_IDR_Picture:
		vt.Value.IFrame = true
		vt.AppendAuBytes(slice)
	case codec.NALU_Non_IDR_Picture,
		codec.NALU_Data_Partition_A,
		codec.NALU_Data_Partition_B,
		codec.NALU_Data_Partition_C:
		vt.Value.IFrame = false
		vt.AppendAuBytes(slice)
	case codec.NALU_SEI:
		vt.AppendAuBytes(slice)
	case codec.NALU_Access_Unit_Delimiter:
	case codec.NALU_Filler_Data:
	default:
		vt.Error("nalu type not support", zap.Int("type", int(naluType)))
	}
}

func (vt *H264) WriteAVCC(ts uint32, frame *util.BLL) (err error) {
	if l := frame.ByteLength; l < 6 {
		vt.Error("AVCC data too short", zap.Int("len", l))
		return io.ErrShortWrite
	}
	if frame.GetByte(1) == 0 {
		vt.WriteSequenceHead(frame.ToBytes())
		frame.Recycle()
		var info codec.AVCDecoderConfigurationRecord
		if _, err = info.Unmarshal(vt.SequenceHead[5:]); err == nil {
			vt.SPSInfo, _ = codec.ParseSPS(info.SequenceParameterSetNALUnit)
			vt.nalulenSize = int(info.LengthSizeMinusOne&3 + 1)
			vt.SPS = info.SequenceParameterSetNALUnit
			vt.PPS = info.PictureParameterSetNALUnit
			vt.ParamaterSets[0] = vt.SPS
			vt.ParamaterSets[1] = vt.PPS
		} else {
			vt.Stream.Error("H264 ParseSpsPps Error")
			vt.Stream.Close()
		}
		return
	} else {
		return vt.Video.WriteAVCC(ts, frame)
	}
}

func (vt *H264) WriteRTPFrame(frame *RTPFrame) {
	if vt.lastSeq != vt.lastSeq2+1 && vt.lastSeq2 != 0 {
		vt.lostFlag = true
		vt.Warn("lost rtp packet", zap.Uint16("lastSeq", vt.lastSeq), zap.Uint16("lastSeq2", vt.lastSeq2))
	}
	rv := vt.Value
	if naluType := frame.H264Type(); naluType < 24 {
		vt.WriteSliceBytes(frame.Payload)
	} else {
		switch naluType {
		case codec.NALU_STAPA, codec.NALU_STAPB:
			for buffer := util.Buffer(frame.Payload[naluType.Offset():]); buffer.CanRead(); {
				nextSize := int(buffer.ReadUint16())
				if buffer.Len() >= nextSize {
					vt.WriteSliceBytes(buffer.ReadN(nextSize))
				} else {
					vt.Error("invalid nalu size", zap.Int("naluType", int(naluType)))
					return
				}
			}
		case codec.NALU_FUA, codec.NALU_FUB:
			if util.Bit1(frame.Payload[1], 0) {
				vt.WriteSliceByte(naluType.Parse(frame.Payload[1]).Or(frame.Payload[0] & 0x60))
			}
			if rv.AUList.Pre != nil && rv.AUList.Pre.Value != nil {
				rv.AUList.Pre.Value.Push(vt.BytesPool.GetShell(frame.Payload[naluType.Offset():]))
			} else {
				vt.Error("fu have no start")
				return
			}
		}
	}
	if frame.Marker && rv.AUList.ByteLength > 0 {
		vt.generateTimestamp(frame.Timestamp)
		if !vt.dcChanged && rv.IFrame {
			vt.insertDCRtp()
		}
		vt.Flush()
	}
}

// RTP格式补完
func (vt *H264) CompleteRTP(value *AVFrame) {
	var out [][][]byte
	if value.IFrame {
		out = append(out, [][]byte{vt.SPS}, [][]byte{vt.PPS})
	}
	vt.Value.AUList.Range(func(au *util.BLL) bool {
		if au.ByteLength < RTPMTU {
			out = append(out, au.ToBuffers())
		} else {
			startIndex := len(out)
			var naluType codec.H264NALUType
			r := au.NewReader()
			b0, _ := r.ReadByte()
			naluType = naluType.Parse(b0)
			b0 = codec.NALU_FUA.Or(b0 & 0x60)
			for bufs := r.ReadN(RTPMTU); len(bufs) > 0; bufs = r.ReadN(RTPMTU) {
				out = append(out, append([][]byte{{b0, naluType.Byte()}}, bufs...))
			}
			out[startIndex][0][1] |= 1 << 7 // set start bit
			out[len(out)-1][0][1] |= 1 << 6 // set end bit
		}
		return true
	})
	vt.PacketizeRTP(out...)
}

func (vt *H264) GetNALU_SEI() (item *util.ListItem[util.Buffer]) {
	item = vt.BytesPool.Get(1)
	item.Value[0] = byte(codec.NALU_SEI)
	return
}
