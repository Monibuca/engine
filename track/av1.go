package track

import (
	"time"

	"github.com/bluenviron/gortsplib/v4/pkg/format/rtpav1"
	"github.com/bluenviron/mediacommon/pkg/codecs/av1"
	"go.uber.org/zap"
	"m7s.live/engine/v4/codec"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/log"
	"m7s.live/engine/v4/util"
)

var _ SpesificTrack = (*AV1)(nil)

type AV1 struct {
	Video
	decoder         rtpav1.Decoder
	encoder         rtpav1.Encoder
	seqHeader       av1.SequenceHeader
	seenFrameHeader bool
	refFrameType    map[byte]byte
}

func NewAV1(puber IPuber, stuff ...any) (vt *AV1) {
	vt = &AV1{}
	vt.Video.CodecID = codec.CodecID_AV1
	vt.SetStuff("av1", byte(96), uint32(90000), vt, stuff, puber)
	if vt.BytesPool == nil {
		vt.BytesPool = make(util.BytesPool, 17)
	}
	vt.nalulenSize = 0
	vt.dtsEst = util.NewDTSEstimator()
	vt.decoder.Init()
	vt.encoder.Init()
	vt.encoder.PayloadType = vt.PayloadType
	vt.ParamaterSets = [][]byte{nil, {0, 0, 0}}
	return
}

func (vt *AV1) WriteSequenceHead(head []byte) (err error) {
	vt.Video.WriteSequenceHead(head)
	var info codec.AV1CodecConfigurationRecord
	info.Unmarshal(head[5:])
	vt.seqHeader.Unmarshal(info.ConfigOBUs)
	vt.ParamaterSets = [][]byte{info.ConfigOBUs, {info.SeqLevelIdx0, info.SeqProfile, info.SeqTier0}}
	return
}

func (vt *AV1) WriteRTPFrame(rtpItem *util.ListItem[RTPFrame]) {
	defer func() {
		err := recover()
		if err != nil {
			vt.Error("WriteRTPFrame panic", zap.Any("err", err))
			vt.Publisher.Stop(zap.Any("err", err))
		}
	}()
	if vt.lastSeq != vt.lastSeq2+1 && vt.lastSeq2 != 0 {
		vt.lostFlag = true
		vt.Warn("lost rtp packet", zap.Uint16("lastSeq", vt.lastSeq), zap.Uint16("lastSeq2", vt.lastSeq2))
	}
	frame := &rtpItem.Value
	rv := vt.Value
	rv.RTP.Push(rtpItem)
	obus, err := vt.decoder.Decode(frame.Packet)
	for _, obu := range obus {
		var obuHeader av1.OBUHeader
		obuHeader.Unmarshal(obu)
		switch obuHeader.Type {
		case av1.OBUTypeSequenceHeader:
			rtmpHead := []byte{0b1001_0000 | byte(codec.PacketTypeMPEG2TSSequenceStart), 0, 0, 0, 0}
			util.BigEndian.PutUint32(rtmpHead[1:], codec.FourCC_AV1_32)
			// TODO: 生成 head
			rtmpHead = append(rtmpHead, obu...)
			vt.Video.WriteSequenceHead(rtmpHead)
			vt.ParamaterSets[0] = obu
		default:
			rv.AUList.Push(vt.BytesPool.GetShell(obu))
		}
	}
	if err == nil {
		vt.generateTimestamp(frame.Timestamp)
		vt.Flush()
	}
}

func (vt *AV1) writeAVCCFrame(ts uint32, r *util.BLLReader, frame *util.BLL) (err error) {
	vt.Value.PTS = time.Duration(ts) * 90
	vt.Value.DTS = time.Duration(ts) * 90
	var obuHeader av1.OBUHeader
	for r.CanRead() {
		offset := r.GetOffset()
		b, _ := r.ReadByte()
		obuHeader.Unmarshal([]byte{b})
		if log.Trace {
			vt.Trace("obu", zap.Any("type", obuHeader.Type), zap.Bool("iframe", vt.Value.IFrame))
		}
		obuSize, _, _ := r.LEB128Unmarshal()
		end := r.GetOffset()
		size := end - offset + int(obuSize)
		r = frame.NewReader()
		r.Skip(offset)
		obu := r.ReadN(size)
		switch obuHeader.Type {
		case codec.AV1_OBU_SEQUENCE_HEADER:
		// 	vt.seqHeader.Unmarshal(util.ConcatBuffers(obu))
		// 	vt.seenFrameHeader = false
		// 	vt.AppendAuBytes(obu...)
		case codec.AV1_OBU_FRAME:
			// 	if !vt.seenFrameHeader {
			// 		if vt.seqHeader.ReducedStillPictureHeader {
			// 			vt.Value.IFrame = true
			// 			vt.seenFrameHeader = true
			// 		} else {
			// 			showframe := obu[0][0] >> 7
			// 			if showframe != 0 {
			// 				frame_to_show_map_idx := (obu[0][0] >> 4) & 0b0111
			// 				vt.Value.IFrame = vt.refFrameType[frame_to_show_map_idx] == 0
			// 			} else {
			// 				vt.Value.IFrame = (obu[0][0])&0b0110_0000 == 0
			// 			}
			// 			vt.seenFrameHeader = showframe == 0
			// 		}
			// 	}
			// 	vt.AppendAuBytes(obu...)
		case codec.AV1_OBU_TEMPORAL_DELIMITER:
		case codec.AV1_OBU_FRAME_HEADER:
		}
		vt.AppendAuBytes(obu...)
	}
	return
}

func (vt *AV1) CompleteAVCC(rv *AVFrame) {
	mem := vt.BytesPool.Get(5)
	b := mem.Value
	if rv.IFrame {
		b[0] = 0b1001_0000 | byte(codec.PacketTypeCodedFrames)
	} else {
		b[0] = 0b1010_0000 | byte(codec.PacketTypeCodedFrames)
	}
	util.BigEndian.PutUint32(b[1:], codec.FourCC_AV1_32)
	// println(rv.PTS < rv.DTS, "\t", rv.PTS, "\t", rv.DTS, "\t", rv.PTS-rv.DTS)
	// 写入CTS
	rv.AVCC.Push(mem)

	rv.AUList.Range(func(au *util.BLL) bool {
		au.Range(func(slice util.Buffer) bool {
			rv.AVCC.Push(vt.BytesPool.GetShell(slice))
			return true
		})
		return true
	})
}

// RTP格式补完
func (vt *AV1) CompleteRTP(value *AVFrame) {
	obus := vt.Value.AUList.ToBuffers()
	// if vt.Value.IFrame {
	// 	obus = append(net.Buffers{vt.ParamaterSets[0]}, obus...)
	// }
	rtps, err := vt.encoder.Encode(obus)
	if err != nil {
		vt.Error("AV1 encoder encode error", zap.Error(err))
		return
	}

	for _, rtp := range rtps {
		vt.Value.RTP.PushValue(RTPFrame{Packet: rtp})
	}
}
