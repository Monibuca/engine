package track

import (
	"io"
	"time"

	"go.uber.org/zap"
	"m7s.live/engine/v4/codec"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/util"
)

var _ SpesificTrack = (*H265)(nil)

type H265 struct {
	Video
	VPS []byte `json:"-"`
}

func NewH265(stream IStream, stuff ...any) (vt *H265) {
	vt = &H265{}
	vt.Video.CodecID = codec.CodecID_H265
	vt.SetStuff("h265", int(256), byte(96), uint32(90000), stream, vt, time.Millisecond*10)
	vt.SetStuff(stuff...)
	vt.ParamaterSets = make(ParamaterSets, 3)
	vt.dtsEst = NewDTSEstimator()
	return
}

func (vt *H265) WriteSliceBytes(slice []byte) {
	switch t := codec.ParseH265NALUType(slice[0]); t {
	case codec.NAL_UNIT_VPS:
		vt.VPS = slice
		vt.ParamaterSets[0] = slice
	case codec.NAL_UNIT_SPS:
		vt.SPS = slice
		vt.ParamaterSets[1] = slice
		vt.SPSInfo, _ = codec.ParseHevcSPS(slice)
	case codec.NAL_UNIT_PPS:
		vt.PPS = slice
		vt.ParamaterSets[2] = slice
		extraData, err := codec.BuildH265SeqHeaderFromVpsSpsPps(vt.VPS, vt.SPS, vt.PPS)
		if err == nil {
			vt.SequenceHead = extraData
			vt.updateSequeceHead()
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
	case codec.NAL_UNIT_SEI:
		vt.AppendAuBytes(slice)
	default:
		vt.Video.Stream.Warn("h265 slice type not supported", zap.Uint("type", uint(t)))
	}
}

func (vt *H265) WriteAVCC(ts uint32, frame util.BLL) (err error) {
	if l := frame.ByteLength; l < 6 {
		vt.Stream.Error("AVCC data too short", zap.Int("len", l))
		return io.ErrShortWrite
	}
	if frame.GetByte(1) == 0 {
		vt.SequenceHead = frame.ToBytes()
		frame.Recycle()
		vt.updateSequeceHead()
		if vt.VPS, vt.SPS, vt.PPS, err = codec.ParseVpsSpsPpsFromSeqHeaderWithoutMalloc(vt.SequenceHead); err == nil {
			vt.SPSInfo, _ = codec.ParseHevcSPS(vt.SequenceHead)
			vt.nalulenSize = (int(vt.SequenceHead[26]) & 0x03) + 1
		} else {
			vt.Stream.Error("H265 ParseVpsSpsPps Error")
			vt.Stream.Close()
		}
		return
	} else {
		return vt.Video.WriteAVCC(ts, frame)
	}
}

func (vt *H265) WriteRTPFrame(frame *RTPFrame) {
	rv := &vt.Value
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
			vt.WriteSliceBytes(buffer.ReadN(int(buffer.ReadUint16())))
			if usingDonlField {
				buffer.ReadByte()
			}
		}
	case codec.NAL_UNIT_RTP_FU:
		first3 := buffer.ReadN(3)
		fuHeader := first3[2]
		if usingDonlField {
			buffer.ReadUint16()
		}
		if naluType := fuHeader & 0b00111111; util.Bit1(fuHeader, 0) {
			vt.WriteSliceByte(first3[0]&0b10000001|(naluType<<1), first3[1])
		}
		rv.AUList.Pre.Value.Push(vt.BytesPool.GetShell(buffer))
	default:
		vt.WriteSliceBytes(frame.Payload)
	}
	frame.SequenceNumber += vt.rtpSequence //增加偏移，需要增加rtp包后需要顺延
}

// RTP格式补完
func (vt *H265) CompleteRTP(value *AVFrame) {
	if len(value.RTP) > 0 {
		if !vt.dcChanged && value.IFrame {
			vt.insertDCRtp()
		}
	} else {
		// H265打包： https://blog.csdn.net/fanyun_01/article/details/114234290
		var out [][][]byte
		if value.IFrame {
			out = append(out, [][]byte{vt.VPS}, [][]byte{vt.SPS}, [][]byte{vt.PPS})
		}
		for au := vt.Value.AUList.Next; au != nil && au != &vt.Value.AUList.ListItem; au = au.Next {
			if au.Value.ByteLength < 1200 {
				out = append(out, au.Value.ToBuffers())
			} else {
				var naluType codec.H265NALUType
				r := au.Value.NewReader()
				b0, _ := r.ReadByte()
				b1, _ := r.ReadByte()
				naluType = naluType.Parse(b0)
				b0 = (byte(codec.NAL_UNIT_RTP_FU) << 1) | (b0 & 0b10000001)
				buf := [][]byte{{b0, b1, (1 << 7) | byte(naluType)}}
				buf = append(buf, r.ReadN(1200-2)...)
				out = append(out, buf)
				for bufs := r.ReadN(1200); len(bufs) > 0; bufs = r.ReadN(1200) {
					buf = append([][]byte{{b0, b1, byte(naluType)}}, bufs...)
					out = append(out, buf)
				}
				buf[0][2] |= 1 << 6 // set end bit
			}
		}
		vt.PacketizeRTP(out...)
	}
}
