package track

import (
	"net"
	"time"

	"github.com/pion/rtp"
	"go.uber.org/zap"
	"m7s.live/engine/v4/codec"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/config"
	"m7s.live/engine/v4/util"
)

type H265 struct {
	Video
}

func NewH265(stream IStream) (vt *H265) {
	vt = &H265{}
	vt.Video.Name = "h265"
	vt.Video.CodecID = codec.CodecID_H265
	vt.Video.SampleRate = 90000
	vt.Video.Stream = stream
	vt.Video.Init(256)
	vt.Video.Media.Poll = time.Millisecond * 10 //适配高帧率
	vt.Video.DecoderConfiguration.PayloadType = 96
	vt.Video.DecoderConfiguration.Raw = make(NALUSlice, 3)
	vt.dtsEst = NewDTSEstimator()
	return
}
func (vt *H265) WriteAnnexB(pts uint32, dts uint32, frame AnnexBFrame) {
	if dts == 0 {
		vt.generateTimestamp(pts)
	} else {
		vt.Video.Media.RingBuffer.Value.PTS = pts
		vt.Video.Media.RingBuffer.Value.DTS = dts
	}
	for _, slice := range vt.Video.WriteAnnexB(frame) {
		vt.WriteSlice(slice)
	}
	if len(vt.Value.Raw) > 0 {
		vt.Flush()
	}
}
func (vt *H265) WriteSlice(slice NALUSlice) {
	switch slice.H265Type() {
	case codec.NAL_UNIT_VPS:
		vt.Video.DecoderConfiguration.Raw[0] = slice[0]
	case codec.NAL_UNIT_SPS:
		vt.Video.DecoderConfiguration.Raw[1] = slice[0]
		vt.Video.SPSInfo, _ = codec.ParseHevcSPS(slice[0])
	case codec.NAL_UNIT_PPS:
		vt.Video.dcChanged = true
		vt.Video.DecoderConfiguration.Raw[2] = slice[0]
		extraData, err := codec.BuildH265SeqHeaderFromVpsSpsPps(vt.Video.DecoderConfiguration.Raw[0], vt.Video.DecoderConfiguration.Raw[1], vt.Video.DecoderConfiguration.Raw[2])
		if err == nil {
			vt.Video.DecoderConfiguration.AVCC = net.Buffers{extraData}
		}
		vt.Video.DecoderConfiguration.FLV = codec.VideoAVCC2FLV(vt.Video.DecoderConfiguration.AVCC, 0)
		vt.Video.DecoderConfiguration.Seq++
	case
		codec.NAL_UNIT_CODED_SLICE_BLA,
		codec.NAL_UNIT_CODED_SLICE_BLANT,
		codec.NAL_UNIT_CODED_SLICE_BLA_N_LP,
		codec.NAL_UNIT_CODED_SLICE_IDR,
		codec.NAL_UNIT_CODED_SLICE_IDR_N_LP,
		codec.NAL_UNIT_CODED_SLICE_CRA:
		vt.Value.IFrame = true
		if vt.sei != nil {
			vt.Video.WriteSlice(vt.sei)
			vt.sei = nil
		}
		vt.Video.WriteSlice(slice)
	case 0, 1, 2, 3, 4, 5, 6, 7, 8, 9:
		vt.Value.IFrame = false
		vt.Video.WriteSlice(slice)
	case codec.NAL_UNIT_SEI:
		vt.sei = slice
	default:
		vt.Video.Stream.Warn("h265 slice type not supported", zap.Uint("type", uint(slice.H265Type())))
	}
}
func (vt *H265) WriteAVCC(ts uint32, frame AVCCFrame) {
	if frame.IsSequence() {
		vt.Video.dcChanged = true
		vt.Video.DecoderConfiguration.Seq++
		vt.Video.DecoderConfiguration.AVCC = net.Buffers{frame}
		if vps, sps, pps, err := codec.ParseVpsSpsPpsFromSeqHeaderWithoutMalloc(frame); err == nil {
			vt.Video.SPSInfo, _ = codec.ParseHevcSPS(frame)
			vt.Video.nalulenSize = (int(frame[26]) & 0x03) + 1
			vt.Video.DecoderConfiguration.Raw[0] = vps
			vt.Video.DecoderConfiguration.Raw[1] = sps
			vt.Video.DecoderConfiguration.Raw[2] = pps
		}
		vt.Video.DecoderConfiguration.FLV = codec.VideoAVCC2FLV(net.Buffers(vt.Video.DecoderConfiguration.AVCC), 0)
	} else {
		vt.Video.WriteAVCC(ts, frame)
		vt.Video.Media.RingBuffer.Value.IFrame = frame.IsIDR()
		vt.Flush()
	}
}

// WriteRTPPack 写入已反序列化的RTP包
func (vt *H265) WriteRTPPack(p *rtp.Packet) {
	for frame := vt.UnmarshalRTPPacket(p); frame != nil; frame = vt.nextRTPFrame() {
		vt.writeRTPFrame(frame)
	}
}

// WriteRTP 写入未反序列化的RTP包
func (vt *H265) WriteRTP(raw []byte) {
	for frame := vt.UnmarshalRTP(raw); frame != nil; frame = vt.nextRTPFrame() {
		vt.writeRTPFrame(frame)
	}
}

func (vt *H265) writeRTPFrame(frame *RTPFrame) {
	rv := &vt.Video.Media.RingBuffer.Value
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
			vt.WriteSlice(NALUSlice{buffer.ReadN(int(buffer.ReadUint16()))})
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
			rv.AppendRaw(NALUSlice{[]byte{first3[0]&0b10000001 | (naluType << 1), first3[1]}})
		}
		lastIndex := len(rv.Raw) - 1
		if lastIndex == -1 {
			return
		}
		rv.Raw[lastIndex].Append(buffer)
		if util.Bit1(fuHeader, 1) {
			complete := rv.Raw[lastIndex] //拼接完成
			rv.Raw = rv.Raw[:lastIndex]   // 缩短一个元素，因为后面的方法会加回去
			vt.WriteSlice(complete)
		}
	default:
		vt.WriteSlice(NALUSlice{frame.Payload})
	}
	rv.AppendRTP(frame)
	if frame.Marker {
		vt.Video.generateTimestamp(frame.Timestamp)
		vt.Flush()
	}
}
func (vt *H265) Flush() {
	if vt.Video.Media.RingBuffer.Value.IFrame {
		vt.Video.ComputeGOP()
	}
	if vt.Attached == 0 && vt.IDRing != nil && vt.DecoderConfiguration.Seq > 0 {
		defer vt.Attach()
	}
	// RTP格式补完
	// H265打包： https://blog.csdn.net/fanyun_01/article/details/114234290
	if vt.Video.Media.RingBuffer.Value.RTP == nil && config.Global.EnableRTP {
		var out [][]byte
		for _, nalu := range vt.Video.Media.RingBuffer.Value.Raw {
			buffers := util.SplitBuffers(nalu, 1200)
			firstBuffer := NALUSlice(buffers[0])
			if l := len(buffers); l == 1 {
				out = append(out, firstBuffer.Bytes())
			} else {
				naluType := firstBuffer.H265Type()
				firstByte := (byte(codec.NAL_UNIT_RTP_FU) << 1) | (firstBuffer[0][0] & 0b10000001)
				buf := []byte{firstByte, firstBuffer[0][1], (1 << 7) | byte(naluType)}
				for i, sp := range firstBuffer {
					if i == 0 {
						sp = sp[2:]
					}
					buf = append(buf, sp...)
				}
				out = append(out, buf)
				for _, bufs := range buffers[1:] {
					buf := []byte{firstByte, firstBuffer[0][1], byte(naluType)}
					for _, sp := range bufs {
						buf = append(buf, sp...)
					}
					out = append(out, buf)
				}
				lastBuf := out[len(out)-1]
				lastBuf[2] |= 1 << 6 // set end bit
			}
		}
		vt.Video.PacketizeRTP(out...)
	}
	vt.Video.Flush()
}
