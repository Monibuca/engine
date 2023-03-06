package engine

import (
	"github.com/pion/rtp/v2"
	"go.uber.org/zap"
	. "m7s.live/engine/v4/codec"
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
	DisableReorder      bool //是否禁用rtp重排序,TCP模式下应当禁用
	mpegps.MpegPsStream `json:"-"`
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
				p.Drop()
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

func (p *PSPublisher) ReceiveVideo(es mpegps.MpegPsEsStream) {
	if p.VideoTrack == nil {
		switch es.Type {
		case mpegts.STREAM_TYPE_H264:
			p.VideoTrack = NewH264(p.Publisher.Stream)
		case mpegts.STREAM_TYPE_H265:
			p.VideoTrack = NewH265(p.Publisher.Stream)
		default:
			//推测编码类型
			var maybe264 H264NALUType
			maybe264 = maybe264.Parse(es.Buffer[4])
			switch maybe264 {
			case NALU_Non_IDR_Picture,
				NALU_IDR_Picture,
				NALU_SEI,
				NALU_SPS,
				NALU_PPS,
				NALU_Access_Unit_Delimiter:
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
