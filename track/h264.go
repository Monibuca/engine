package track

import (
	"net"
	"time"

	"github.com/pion/rtp/v2"
	"m7s.live/engine/v4/codec"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/config"
	"m7s.live/engine/v4/util"
)

type H264 struct {
	Video
	dtsEst *DTSEstimator
}

func NewH264(stream IStream) (vt *H264) {
	vt = &H264{}
	vt.Name = "h264"
	vt.CodecID = codec.CodecID_H264
	vt.SampleRate = 90000
	vt.Stream = stream
	vt.Init(256)
	vt.Poll = time.Millisecond * 20
	vt.DecoderConfiguration.PayloadType = 96
	vt.DecoderConfiguration.Raw = make(NALUSlice, 2)
	if config.Global.RTPReorder {
		vt.orderQueue = make([]*RTPFrame, 20)
	}
	vt.dtsEst = NewDTSEstimator()
	return
}
func (vt *H264) WriteAnnexB(pts uint32, dts uint32, frame AnnexBFrame) {
	for _, slice := range vt.Video.WriteAnnexB(pts, dts, frame) {
		vt.WriteSlice(slice)
	}
	vt.Flush()
}
func (vt *H264) WriteSlice(slice NALUSlice) {
	switch slice.H264Type() {
	case codec.NALU_SPS:
		vt.SPSInfo, _ = codec.ParseSPS(slice[0])
		vt.DecoderConfiguration.Raw[0] = slice[0]
	case codec.NALU_PPS:
		vt.DecoderConfiguration.Raw[1] = slice[0]
		lenSPS := len(vt.DecoderConfiguration.Raw[0])
		lenPPS := len(vt.DecoderConfiguration.Raw[1])
		if lenSPS > 3 {
			vt.DecoderConfiguration.AVCC = net.Buffers{codec.RTMP_AVC_HEAD[:6], vt.DecoderConfiguration.Raw[0][1:4], codec.RTMP_AVC_HEAD[9:10]}
		} else {
			vt.DecoderConfiguration.AVCC = net.Buffers{codec.RTMP_AVC_HEAD}
		}
		tmp := []byte{0xE1, 0, 0, 0x01, 0, 0}
		util.PutBE(tmp[1:3], lenSPS)
		util.PutBE(tmp[4:6], lenPPS)
		vt.DecoderConfiguration.AVCC = append(vt.DecoderConfiguration.AVCC, tmp[:3], vt.DecoderConfiguration.Raw[0], tmp[3:], vt.DecoderConfiguration.Raw[1])
		vt.DecoderConfiguration.FLV = codec.VideoAVCC2FLV(vt.DecoderConfiguration.AVCC, 0)

	case codec.NALU_IDR_Picture:
		vt.Value.IFrame = true
		fallthrough
	case codec.NALU_Non_IDR_Picture,
		codec.NALU_SEI:
		vt.Media.WriteSlice(slice)
	}
}

func (vt *H264) WriteAVCC(ts uint32, frame AVCCFrame) {
	if frame.IsSequence() {
		vt.DecoderConfiguration.AVCC = net.Buffers{frame}
		var info codec.AVCDecoderConfigurationRecord
		if _, err := info.Unmarshal(frame[5:]); err == nil {
			vt.SPSInfo, _ = codec.ParseSPS(info.SequenceParameterSetNALUnit)
			vt.nalulenSize = int(info.LengthSizeMinusOne&3 + 1)
			vt.DecoderConfiguration.Raw[0] = info.SequenceParameterSetNALUnit
			vt.DecoderConfiguration.Raw[1] = info.PictureParameterSetNALUnit
		}
		vt.DecoderConfiguration.FLV = codec.VideoAVCC2FLV(net.Buffers(vt.DecoderConfiguration.AVCC), 0)
	} else {
		vt.Video.WriteAVCC(ts, frame)
		vt.Value.IFrame = frame.IsIDR()
		vt.Flush()
	}
}
func (vt *H264) writeRTPFrame(frame *RTPFrame) {
	if naluType := frame.H264Type(); naluType < 24 {
		vt.WriteSlice(NALUSlice{frame.Payload})
	} else {
		switch naluType {
		case codec.NALU_STAPA, codec.NALU_STAPB:
			for buffer := util.Buffer(frame.Payload[naluType.Offset():]); buffer.CanRead(); {
				vt.WriteSlice(NALUSlice{buffer.ReadN(int(buffer.ReadUint16()))})
			}
		case codec.NALU_FUA, codec.NALU_FUB:
			if util.Bit1(frame.Payload[1], 0) {
				vt.Value.AppendRaw(NALUSlice{[]byte{naluType.Parse(frame.Payload[1]).Or(frame.Payload[0] & 0x60)}})
			}
			// 最后一个是半包缓存，用于拼接
			lastIndex := len(vt.Value.Raw) - 1
			if lastIndex == -1 {
				return
			}
			vt.Value.Raw[lastIndex].Append(frame.Payload[naluType.Offset():])
			if util.Bit1(frame.Payload[1], 1) {
				complete := vt.Value.Raw[lastIndex]     //拼接完成
				vt.Value.Raw = vt.Value.Raw[:lastIndex] // 缩短一个元素，因为后面的方法会加回去
				vt.WriteSlice(complete)
			}
		}
	}
	vt.Value.AppendRTP(frame)
	if frame.Marker {
		vt.Value.PTS = frame.Timestamp
		vt.Value.DTS = vt.dtsEst.Feed(frame.Timestamp)
		vt.Flush()
	}
}

// WriteRTPPack 写入已反序列化的RTP包
func (vt *H264) WriteRTPPack(p *rtp.Packet) {
	for frame := vt.UnmarshalRTPPacket(p); frame != nil; frame = vt.nextRTPFrame() {
		vt.writeRTPFrame(frame)
	}
}

// WriteRTP 写入未反序列化的RTP包
func (vt *H264) WriteRTP(raw []byte) {
	for frame := vt.UnmarshalRTP(raw); frame != nil; frame = vt.nextRTPFrame() {
		vt.writeRTPFrame(frame)
	}
}

func (vt *H264) Flush() {
	if vt.Value.IFrame {
		if vt.IDRing == nil {
			defer vt.Attach()
		}
		vt.Video.ComputeGOP()
	}
	// RTP格式补完
	if vt.Value.RTP == nil && config.Global.EnableRTP {
		var out [][]byte
		for _, nalu := range vt.Value.Raw {
			buffers := util.SplitBuffers(nalu, 1200)
			firstBuffer := NALUSlice(buffers[0])
			if l := len(buffers); l == 1 {
				out = append(out, firstBuffer.Bytes())
			} else {
				naluType := firstBuffer.H264Type()
				firstByte := codec.NALU_FUA.Or(firstBuffer.RefIdc())
				buf := []byte{firstByte, naluType.Or(1 << 7)}
				for i, sp := range firstBuffer {
					if i == 0 {
						sp = sp[1:]
					}
					buf = append(buf, sp...)
				}
				out = append(out, buf)
				for _, bufs := range buffers[1:] {
					buf := []byte{firstByte, naluType.Byte()}
					for _, sp := range bufs {
						buf = append(buf, sp...)
					}
					out = append(out, buf)
				}
				lastBuf := out[len(out)-1]
				lastBuf[1] |= 1 << 6 // set end bit
			}
		}
		vt.PacketizeRTP(out...)
	}
	vt.Video.Flush()
}
