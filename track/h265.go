package track

import (
	"net"
	"time"

	"go.uber.org/zap"
	"m7s.live/engine/v4/codec"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/util"
)

var _ SpesificTrack[NALUSlice] = (*H265)(nil)

type H265 struct {
	Video
}

func NewH265(stream IStream) (vt *H265) {
	vt = &H265{}
	vt.Video.CodecID = codec.CodecID_H265
	vt.Video.DecoderConfiguration.Raw = make(NALUSlice, 3)
	vt.SetStuff("h265", stream, int(256), byte(96), uint32(90000), vt, time.Millisecond*10)
	vt.dtsEst = NewDTSEstimator()
	return
}

func (vt *H265) WriteSliceBytes(slice []byte) {
	switch t := codec.ParseH265NALUType(slice[0]); t {
	case codec.NAL_UNIT_VPS:
		vt.Video.DecoderConfiguration.Raw[0] = slice
	case codec.NAL_UNIT_SPS:
		vt.Video.DecoderConfiguration.Raw[1] = slice
		vt.Video.SPSInfo, _ = codec.ParseHevcSPS(slice)
	case codec.NAL_UNIT_PPS:
		vt.Video.dcChanged = true
		vt.Video.DecoderConfiguration.Raw[2] = slice
		extraData, err := codec.BuildH265SeqHeaderFromVpsSpsPps(vt.Video.DecoderConfiguration.Raw[0], vt.Video.DecoderConfiguration.Raw[1], vt.Video.DecoderConfiguration.Raw[2])
		if err == nil {
			vt.Video.DecoderConfiguration.AVCC = net.Buffers{extraData}
		}
		vt.Video.DecoderConfiguration.Seq++
	case
		codec.NAL_UNIT_CODED_SLICE_BLA,
		codec.NAL_UNIT_CODED_SLICE_BLANT,
		codec.NAL_UNIT_CODED_SLICE_BLA_N_LP,
		codec.NAL_UNIT_CODED_SLICE_IDR,
		codec.NAL_UNIT_CODED_SLICE_IDR_N_LP,
		codec.NAL_UNIT_CODED_SLICE_CRA:
		vt.Value.IFrame = true
		vt.WriteRawBytes(slice)
	case 0, 1, 2, 3, 4, 5, 6, 7, 8, 9:
		vt.Value.IFrame = false
		vt.WriteRawBytes(slice)
	case codec.NAL_UNIT_SEI:
		vt.WriteRawBytes(slice)
	default:
		vt.Video.Stream.Warn("h265 slice type not supported", zap.Uint("type", uint(t)))
	}
}

func (vt *H265) WriteAVCC(ts uint32, frame AVCCFrame) {
	if l := util.SizeOfBuffers(frame); l < 6 {
		vt.Stream.Error("AVCC data too short", zap.Int("len", l))
		return
	}
	if frame.IsSequence() {
		vt.Video.dcChanged = true
		vt.Video.DecoderConfiguration.Seq++
		vt.Video.DecoderConfiguration.AVCC = net.Buffers(frame)
		if vps, sps, pps, err := codec.ParseVpsSpsPpsFromSeqHeaderWithoutMalloc(frame[0]); err == nil {
			vt.Video.SPSInfo, _ = codec.ParseHevcSPS(frame[0])
			vt.Video.nalulenSize = (int(frame[0][26]) & 0x03) + 1
			vt.Video.DecoderConfiguration.Raw[0] = vps
			vt.Video.DecoderConfiguration.Raw[1] = sps
			vt.Video.DecoderConfiguration.Raw[2] = pps
		} else {
			vt.Stream.Error("H265 ParseVpsSpsPps Error")
			vt.Stream.Close()
		}
	} else {
		vt.Video.WriteAVCC(ts, frame)
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
		lastIndex := len(rv.Raw) - 1
		if lastIndex == -1 {
			return
		}
		rv.Raw[lastIndex].Append(buffer)
		// if util.Bit1(fuHeader, 1) {
		// 	complete := rv.Raw[lastIndex] //拼接完成
		// 	rv.Raw = rv.Raw[:lastIndex]   // 缩短一个元素，因为后面的方法会加回去
		// 	vt.WriteSlice(complete)
		// }
	default:
		vt.WriteSliceBytes(frame.Payload)
	}
	frame.SequenceNumber += vt.rtpSequence //增加偏移，需要增加rtp包后需要顺延
}

// RTP格式补完
func (vt *H265) CompleteRTP(value *AVFrame[NALUSlice]) {
	if len(value.RTP) > 0 {
		if !vt.dcChanged && value.IFrame {
			vt.insertDCRtp()
		}
	} else {
		// H265打包： https://blog.csdn.net/fanyun_01/article/details/114234290
		var out [][][]byte
		if value.IFrame {
			out = append(out, [][]byte{vt.DecoderConfiguration.Raw[0]}, [][]byte{vt.DecoderConfiguration.Raw[1]}, [][]byte{vt.DecoderConfiguration.Raw[2]})
		}
		for _, nalu := range vt.Video.Media.RingBuffer.Value.Raw {
			buffers := util.SplitBuffers(nalu, 1200)
			firstBuffer := NALUSlice(buffers[0])
			if l := len(buffers); l == 1 {
				out = append(out, firstBuffer)
			} else {
				naluType := firstBuffer.H265Type()
				firstByte := (byte(codec.NAL_UNIT_RTP_FU) << 1) | (firstBuffer[0][0] & 0b10000001)
				buf := [][]byte{{firstByte, firstBuffer[0][1], (1 << 7) | byte(naluType)}}
				for i, sp := range firstBuffer {
					if i == 0 {
						sp = sp[2:]
					}
					buf = append(buf, sp)
				}
				out = append(out, buf)
				for _, bufs := range buffers[1:] {
					buf = append([][]byte{{firstByte, firstBuffer[0][1], byte(naluType)}}, bufs...)
					out = append(out, buf)
				}
				buf[0][2] |= 1 << 6 // set end bit
			}
		}
		vt.PacketizeRTP(out...)
	}
}
