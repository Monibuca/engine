package engine

import (
	"github.com/pion/rtp/v2"
	"github.com/yapingcat/gomedia/go-mpeg2"
	"go.uber.org/zap"
	"m7s.live/engine/v4/codec"
	"m7s.live/engine/v4/codec/mpegps"
	"m7s.live/engine/v4/codec/mpegts"
	. "m7s.live/engine/v4/track"
	"m7s.live/engine/v4/util"
)

type cacheItem struct {
	Seq uint16
	*util.ListItem[util.Buffer]
}

type PSPublisher struct {
	Publisher
	DisableReorder bool //是否禁用rtp重排序,TCP模式下应当禁用
	// mpegps.MpegPsStream `json:"-"`
	// *mpegps.PSDemuxer `json:"-"`
	mpegps.DecPSPackage `json:"-"`
	reorder             util.RTPReorder[*cacheItem]
	pool                util.BytesPool
	lastSeq             uint16
}

// 解析rtp封装 https://www.ietf.org/rfc/rfc2250.txt
func (p *PSPublisher) PushPS(rtp *rtp.Packet) {
	if p.Stream == nil {
		return
	}
	if p.EsHandler == nil {
		// p.PSDemuxer = mpegps.NewPSDemuxer()
		// p.PSDemuxer.OnPacket = p.OnPacket
		// p.PSDemuxer.OnFrame = p.OnFrame
		p.EsHandler = p
		p.lastSeq = rtp.SequenceNumber - 1
		if p.pool == nil {
			p.pool = make(util.BytesPool, 17)
		}
	}
	if p.DisableReorder {
		p.Feed(rtp.Payload)
		p.lastSeq = rtp.SequenceNumber
	} else {
		item := p.pool.Get(len(rtp.Payload))
		copy(item.Value, rtp.Payload)
		for cacheItem := p.reorder.Push(rtp.SequenceNumber, &cacheItem{rtp.SequenceNumber, item}); cacheItem != nil; cacheItem = p.reorder.Pop() {
			if cacheItem.Seq != p.lastSeq+1 {
				p.Debug("drop", zap.Uint16("seq", cacheItem.Seq), zap.Uint16("lastSeq", p.lastSeq))
				p.Reset()
				if p.VideoTrack != nil {
					p.SetLostFlag()
				}
			}
			p.Feed(cacheItem.Value)
			p.lastSeq = cacheItem.Seq
			cacheItem.Recycle()
		}
	}
}
func (p *PSPublisher) OnFrame(frame []byte, cid mpeg2.PS_STREAM_TYPE, pts uint64, dts uint64) {
	switch cid {
	case mpeg2.PS_STREAM_AAC:
		if p.AudioTrack != nil {
			p.AudioTrack.WriteADTS(uint32(pts), frame)
		} else {
			p.AudioTrack = NewAAC(p.Publisher.Stream)
		}
	case mpeg2.PS_STREAM_G711A:
		if p.AudioTrack != nil {
			p.AudioTrack.WriteRaw(uint32(pts), frame)
		} else {
			p.AudioTrack = NewG711(p.Publisher.Stream, true)
		}
	case mpeg2.PS_STREAM_G711U:
		if p.AudioTrack != nil {
			p.AudioTrack.WriteRaw(uint32(pts), frame)
		} else {
			p.AudioTrack = NewG711(p.Publisher.Stream, false)
		}
	case mpeg2.PS_STREAM_H264:
		if p.VideoTrack != nil {
			// p.WriteNalu(uint32(pts), uint32(dts), frame)
			p.WriteAnnexB(uint32(pts), uint32(dts), frame)
		} else {
			p.VideoTrack = NewH264(p.Publisher.Stream)
		}
	case mpeg2.PS_STREAM_H265:
		if p.VideoTrack != nil {
			// p.WriteNalu(uint32(pts), uint32(dts), frame)
			p.WriteAnnexB(uint32(pts), uint32(dts), frame)
		} else {
			p.VideoTrack = NewH265(p.Publisher.Stream)
		}
	}
}

func (p *PSPublisher) OnPacket(pkg mpeg2.Display, decodeResult error) {
	// switch value := pkg.(type) {
	// case *mpeg2.PSPackHeader:
	// 	// fd3.WriteString("--------------PS Pack Header--------------\n")
	// 	if decodeResult == nil {
	// 		// value.PrettyPrint(fd3)
	// 	} else {
	// 		// fd3.WriteString(fmt.Sprintf("Decode Ps Packet Failed %s\n", decodeResult.Error()))
	// 	}
	// case *mpeg2.System_header:
	// 	// fd3.WriteString("--------------System Header--------------\n")
	// 	if decodeResult == nil {
	// 		// value.PrettyPrint(fd3)
	// 	} else {
	// 		// fd3.WriteString(fmt.Sprintf("Decode Ps Packet Failed %s\n", decodeResult.Error()))
	// 	}
	// case *mpeg2.Program_stream_map:
	// 	// fd3.WriteString("--------------------PSM-------------------\n")
	// 	if decodeResult == nil {
	// 		// value.PrettyPrint(fd3)
	// 	} else {
	// 		// fd3.WriteString(fmt.Sprintf("Decode Ps Packet Failed %s\n", decodeResult.Error()))
	// 	}
	// case *mpeg2.PesPacket:
	// 	// fd3.WriteString("-------------------PES--------------------\n")
	// 	if decodeResult == nil {
	// 		// value.PrettyPrint(fd3)
	// 	} else {
	// 		// fd3.WriteString(fmt.Sprintf("Decode Ps Packet Failed %s\n", decodeResult.Error()))
	// 	}
	// }
}

func (p *PSPublisher) ReceiveVideo(es mpegps.MpegPsEsStream) {
	if p.VideoTrack == nil {
		switch es.Type {
		case mpegts.STREAM_TYPE_H264:
			p.VideoTrack = NewH264(p.Publisher.Stream)
		case mpegts.STREAM_TYPE_H265:
			p.VideoTrack = NewH265(p.Publisher.Stream)
		default:
			//推测编码类型
			var maybe264 codec.H264NALUType
			maybe264 = maybe264.Parse(es.Buffer[4])
			switch maybe264 {
			case codec.NALU_Non_IDR_Picture,
				codec.NALU_IDR_Picture,
				codec.NALU_SEI,
				codec.NALU_SPS,
				codec.NALU_PPS,
				codec.NALU_Access_Unit_Delimiter:
				p.VideoTrack = NewH264(p.Publisher.Stream)
			default:
				p.Info("maybe h265", zap.Uint8("type", maybe264.Byte()))
				p.VideoTrack = NewH265(p.Publisher.Stream)
			}
		}
	}
	payload, pts, dts := es.Buffer, es.PTS, es.DTS
	if dts == 0 {
		dts = pts
	}
	// if binary.BigEndian.Uint32(payload) != 1 {
	// 	panic("not annexb")
	// }
	p.WriteAnnexB(pts, dts, payload)
}

func (p *PSPublisher) ReceiveAudio(es mpegps.MpegPsEsStream) {
	ts, payload := es.PTS, es.Buffer
	if p.AudioTrack == nil {
		switch es.Type {
		case mpegts.STREAM_TYPE_G711A:
			p.AudioTrack = NewG711(p.Publisher.Stream, true)
		case mpegts.STREAM_TYPE_G711U:
			p.AudioTrack = NewG711(p.Publisher.Stream, false)
		case mpegts.STREAM_TYPE_AAC:
			p.AudioTrack = NewAAC(p.Publisher.Stream)
			p.WriteADTS(ts, payload)
		case 0: //推测编码类型
			if payload[0] == 0xff && payload[1]>>4 == 0xf {
				p.AudioTrack = NewAAC(p.Publisher.Stream)
				p.WriteADTS(ts, payload)
			}
		default:
			p.Error("audio type not supported yet", zap.Uint8("type", es.Type))
		}
	} else if es.Type == mpegts.STREAM_TYPE_AAC {
		p.WriteADTS(ts, payload)
	} else {
		p.WriteRaw(ts, payload)
	}
}
