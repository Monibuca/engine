package track

import (
	"bytes"
	"time"

	"go.uber.org/zap"
	"m7s.live/engine/v4/codec"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/log"
	"m7s.live/engine/v4/util"
)

var _ SpesificTrack = (*H264)(nil)

type H264 struct {
	Video
	buf util.Buffer // rtp 包临时缓存,对于不规范的 rtp 包（sps 放到了 fua 中导致）需要缓存
}

func NewH264(stream IStream, stuff ...any) (vt *H264) {
	vt = &H264{}
	vt.Video.CodecID = codec.CodecID_H264
	vt.SetStuff("h264", byte(96), uint32(90000), vt, stuff, stream)
	if vt.BytesPool == nil {
		vt.BytesPool = make(util.BytesPool, 17)
	}
	vt.ParamaterSets = make(ParamaterSets, 2)
	vt.nalulenSize = 4
	vt.dtsEst = NewDTSEstimator()
	return
}

func (vt *H264) WriteSliceBytes(slice []byte) {
	if len(slice) > 4 && bytes.Equal(slice[:4], codec.NALU_Delimiter2) {
		slice = slice[4:] // 有些设备厂商不规范，所以需要移除前导的 00 00 00 01
	}
	if len(slice) == 0 {
		vt.Error("H264 WriteSliceBytes got empty slice")
		return
	}
	naluType := codec.ParseH264NALUType(slice[0])
	if log.Trace {
		vt.Trace("naluType", zap.Uint8("naluType", naluType.Byte()))
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
		if vt.Value.IFrame {
			vt.AppendAuBytes(slice)
			return
		}
		vt.Error("nalu type not support", zap.Int("type", int(naluType)))
	}
}
func (vt *H264) WriteSequenceHead(head []byte) (err error) {
	var info codec.AVCDecoderConfigurationRecord
	if _, err = info.Unmarshal(head[5:]); err == nil {
		vt.SPSInfo, _ = codec.ParseSPS(info.SequenceParameterSetNALUnit)
		vt.nalulenSize = int(info.LengthSizeMinusOne&3 + 1)
		vt.SPS = info.SequenceParameterSetNALUnit
		vt.PPS = info.PictureParameterSetNALUnit
		vt.ParamaterSets[0] = vt.SPS
		vt.ParamaterSets[1] = vt.PPS
		vt.Video.WriteSequenceHead(head)
	} else {
		vt.Stream.Error("H264 ParseSpsPps Error")
		vt.Stream.Close()
	}
	return
}

func (vt *H264) WriteRTPFrame(rtpItem *util.ListItem[RTPFrame]) {
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
	pts := frame.Timestamp
	rv := vt.Value
	// 有些流的 rtp 包中没有设置 marker 导致无法判断是否是最后一个包，此时通过时间戳变化判断，先 flush 之前的帧
	if rv.PTS != time.Duration(pts) {
		if rv.AUList.ByteLength > 0 {
			if !vt.dcChanged && rv.IFrame {
				vt.insertDCRtp()
			}
			vt.Flush()
			rv = vt.Value
		}
		vt.generateTimestamp(pts)
	}
	rv.RTP.Push(rtpItem)
	if naluType := frame.H264Type(); naluType < 24 {
		vt.WriteSliceBytes(frame.Payload)
	} else {
		offset := naluType.Offset()
		switch naluType {
		case codec.NALU_STAPA, codec.NALU_STAPB:
			if len(frame.Payload) <= offset {
				vt.Error("invalid nalu size", zap.Int("naluType", int(naluType)))
				return
			}
			for buffer := util.Buffer(frame.Payload[offset:]); buffer.CanRead(); {
				nextSize := int(buffer.ReadUint16())
				if buffer.Len() >= nextSize {
					vt.WriteSliceBytes(buffer.ReadN(nextSize))
				} else {
					vt.Error("invalid nalu size", zap.Int("naluType", int(naluType)))
					return
				}
			}
		case codec.NALU_FUA, codec.NALU_FUB:
			b1 := frame.Payload[1]
			if util.Bit1(b1, 0) {
				naluType = naluType.Parse(b1)
				firstByte := naluType.Or(frame.Payload[0] & 0x60)
				switch naluType {
				case codec.NALU_SPS, codec.NALU_PPS:
					vt.buf.WriteByte(firstByte)
				default:
					vt.WriteSliceByte(firstByte)
				}
			}
			if vt.buf.Len() > 0 {
				vt.buf.Write(frame.Payload[offset:])
			} else {
				if rv.AUList.Pre != nil && rv.AUList.Pre.Value != nil {
					rv.AUList.Pre.Value.Push(vt.BytesPool.GetShell(frame.Payload[offset:]))
				} else {
					vt.Error("fu have no start")
					return
				}
			}
			if !util.Bit1(b1, 1) {
				// fua 还没结束
				return
			} else if vt.buf.Len() > 0 {
				vt.WriteAnnexB(uint32(rv.PTS), uint32(rv.DTS), vt.buf)
				vt.buf = nil
			}
		}
	}
	if frame.Marker && rv.AUList.ByteLength > 0 {
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
