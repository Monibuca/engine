package track

import (
	"go.uber.org/zap"
	"m7s.live/engine/v4/codec"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/log"
	"m7s.live/engine/v4/util"
)

var _ SpesificTrack = (*H265)(nil)

type H265 struct {
	Video
	VPS []byte `json:"-" yaml:"-"`
}

func NewH265(stream IStream, stuff ...any) (vt *H265) {
	vt = &H265{}
	vt.Video.CodecID = codec.CodecID_H265
	vt.SetStuff("h265", byte(96), uint32(90000), vt, stuff, stream)
	if vt.BytesPool == nil {
		vt.BytesPool = make(util.BytesPool, 17)
	}
	vt.ParamaterSets = make(ParamaterSets, 3)
	vt.nalulenSize = 4
	vt.dtsEst = NewDTSEstimator()
	return
}

func (vt *H265) WriteSliceBytes(slice []byte) {
	if len(slice) == 0 {
		vt.Error("H265 WriteSliceBytes got empty slice")
		return
	}
	t := codec.ParseH265NALUType(slice[0])
	if log.Trace {
		vt.Trace("naluType", zap.Uint8("naluType", byte(t)))
	}
	switch t {
	case codec.NAL_UNIT_VPS:
		vt.VPS = slice
		vt.ParamaterSets[0] = slice
	case codec.NAL_UNIT_SPS:
		vt.SPS = slice
		vt.ParamaterSets[1] = slice
		spsInfo, _ := codec.ParseHevcSPS(slice)
		if spsInfo.Width != vt.SPSInfo.Width || spsInfo.Height != vt.SPSInfo.Height {
			vt.Debug("SPS", zap.Any("SPSInfo", spsInfo))
		}
		vt.SPSInfo = spsInfo
	case codec.NAL_UNIT_PPS:
		vt.PPS = slice
		vt.ParamaterSets[2] = slice
		if vt.VPS != nil && vt.SPS != nil && vt.PPS != nil {
			extraData, err := codec.BuildH265SeqHeaderFromVpsSpsPps(vt.VPS, vt.SPS, vt.PPS)
			if err == nil {
				vt.nalulenSize = (int(extraData[26]) & 0x03) + 1
				vt.Video.WriteSequenceHead(extraData)
			} else {
				vt.Error("H265 BuildH265SeqHeaderFromVpsSpsPps", zap.Error(err))
				// vt.Stream.Close()
			}
		}
	case
		codec.NAL_UNIT_CODED_SLICE_BLA,
		codec.NAL_UNIT_CODED_SLICE_BLANT,
		codec.NAL_UNIT_CODED_SLICE_BLA_N_LP,
		codec.NAL_UNIT_CODED_SLICE_IDR,
		codec.NAL_UNIT_CODED_SLICE_IDR_N_LP,
		codec.NAL_UNIT_CODED_SLICE_CRA:
		vt.Value.IFrame = true
		vt.AppendAuBytes(slice)
	case 0, 1, 2, 3, 4, 5, 6, 7, 8, 9:
		vt.Value.IFrame = false
		vt.AppendAuBytes(slice)
	case codec.NAL_UNIT_SEI, codec.NAL_UNIT_SEI_SUFFIX:
		vt.AppendAuBytes(slice)
	case codec.NAL_UNIT_ACCESS_UNIT_DELIMITER:
	default:
		vt.Warn("nalu type not supported", zap.Uint("type", uint(t)))
	}
}

func (vt *H265) WriteSequenceHead(head []byte) (err error) {
	if vt.VPS, vt.SPS, vt.PPS, err = codec.ParseVpsSpsPpsFromSeqHeaderWithoutMalloc(head); err == nil {
		vt.ParamaterSets[0] = vt.VPS
		vt.ParamaterSets[1] = vt.SPS
		vt.ParamaterSets[2] = vt.PPS
		vt.SPSInfo, _ = codec.ParseHevcSPS(vt.SPS)
		vt.nalulenSize = (int(head[26]) & 0x03) + 1
		vt.Video.WriteSequenceHead(head)
	} else {
		vt.Error("H265 ParseVpsSpsPps Error")
		vt.Stream.Close()
	}
	return
}

func (vt *H265) WriteRTPFrame(rtpItem *util.ListItem[RTPFrame]) {
	defer func() {
		err := recover()
		if err != nil {
			vt.Error("WriteRTPFrame panic", zap.Any("err", err))
			vt.Stream.Close()
		}
	}()
	frame := &rtpItem.Value
	rv := vt.Value
	rv.RTP.Push(rtpItem)
	// TODO: DONL may need to be parsed if `sprop-max-don-diff` is greater than 0 on the RTP stream.
	var usingDonlField bool
	var buffer = util.Buffer(frame.Payload)
	switch frame.H265Type() {
	case codec.NAL_UNIT_RTP_AP:
		buffer.ReadUint16()
		if usingDonlField {
			buffer.ReadUint16()
		}
		for buffer.CanRead() {
			l := int(buffer.ReadUint16())
			if buffer.CanReadN(l) {
				vt.WriteSliceBytes(buffer.ReadN(l))
			} else {
				return
			}
			if usingDonlField {
				buffer.ReadByte()
			}
		}
	case codec.NAL_UNIT_RTP_FU:
		if !buffer.CanReadN(3) {
			return
		}
		first3 := buffer.ReadN(3)
		fuHeader := first3[2]
		if usingDonlField {
			buffer.ReadUint16()
		}
		if naluType := fuHeader & 0b00111111; util.Bit1(fuHeader, 0) {
			vt.WriteSliceByte(first3[0]&0b10000001|(naluType<<1), first3[1])
		}
		if rv.AUList.Pre != nil {
			rv.AUList.Pre.Value.Push(vt.BytesPool.GetShell(buffer))
		}
	default:
		vt.WriteSliceBytes(frame.Payload)
	}
	if frame.Marker {
		vt.generateTimestamp(frame.Timestamp)
		if !vt.dcChanged && rv.IFrame {
			vt.insertDCRtp()
		}
		vt.Flush()
	}
}

func (vt *H265) CompleteAVCC(rv *AVFrame) {
	mem := vt.BytesPool.Get(8)
	b := mem.Value
	if rv.IFrame {
		b[0] = 0b1001_0000 | byte(codec.PacketTypeCodedFrames)
	} else {
		b[0] = 0b1010_0000 | byte(codec.PacketTypeCodedFrames)
	}
	util.BigEndian.PutUint32(b[1:], codec.FourCC_H265_32)
	// println(rv.PTS < rv.DTS, "\t", rv.PTS, "\t", rv.DTS, "\t", rv.PTS-rv.DTS)
	// 写入CTS
	util.PutBE(b[5:8], (rv.PTS-rv.DTS)/90)
	rv.AVCC.Push(mem)
	// if rv.AVCC.ByteLength != 5 {
	// 	panic("error")
	// }
	// var tmp = 0
	rv.AUList.Range(func(au *util.BLL) bool {
		mem = vt.BytesPool.Get(4)
		// println(au.ByteLength)
		util.PutBE(mem.Value, uint32(au.ByteLength))
		rv.AVCC.Push(mem)
		au.Range(func(slice util.Buffer) bool {
			rv.AVCC.Push(vt.BytesPool.GetShell(slice))
			return true
		})
		// tmp += 4 + au.ByteLength
		// if rv.AVCC.ByteLength != 5+tmp {
		// 	panic("error")
		// }
		return true
	})
}

// RTP格式补完
func (vt *H265) CompleteRTP(value *AVFrame) {
	// H265打包： https://blog.csdn.net/fanyun_01/article/details/114234290
	var out [][][]byte
	if value.IFrame {
		out = append(out, [][]byte{vt.VPS}, [][]byte{vt.SPS}, [][]byte{vt.PPS})
	}
	vt.Value.AUList.Range(func(au *util.BLL) bool {
		if au.ByteLength < RTPMTU {
			out = append(out, au.ToBuffers())
		} else {
			startIndex := len(out)
			var naluType codec.H265NALUType
			r := au.NewReader()
			b0, _ := r.ReadByte()
			b1, _ := r.ReadByte()
			naluType = naluType.Parse(b0)
			b0 = (byte(codec.NAL_UNIT_RTP_FU) << 1) | (b0 & 0b10000001)
			for bufs := r.ReadN(RTPMTU); len(bufs) > 0; bufs = r.ReadN(RTPMTU) {
				out = append(out, append([][]byte{{b0, b1, byte(naluType)}}, bufs...))
			}
			out[startIndex][0][2] |= 1 << 7 // set start bit
			out[len(out)-1][0][2] |= 1 << 6 // set end bit
		}
		return true
	})
	vt.PacketizeRTP(out...)
}

func (vt *H265) GetNALU_SEI() (item *util.ListItem[util.Buffer]) {
	item = vt.BytesPool.Get(1)
	item.Value[0] = 0b10000000 | byte(codec.NAL_UNIT_SEI<<1)
	return
}
