package engine

import (
	"go.uber.org/zap"
	"m7s.live/engine/v4/codec/mpegts"
	"m7s.live/engine/v4/track"
	"m7s.live/engine/v4/util"
)

type TSReader struct {
	*TSPublisher
	mpegts.MpegTsStream
}

func NewTSReader(pub *TSPublisher) (r *TSReader) {
	r = &TSReader{
		TSPublisher: pub,
	}
	r.PESChan = make(chan *mpegts.MpegTsPESPacket, 50)
	r.PESBuffer = make(map[uint16]*mpegts.MpegTsPESPacket)
	go r.ReadPES()
	return
}

type TSPublisher struct {
	Publisher
	pool util.BytesPool
}

func (t *TSPublisher) OnEvent(event any) {
	switch v := event.(type) {
	case IPublisher:
		t.pool = make(util.BytesPool, 17)
		if v.GetPublisher() != &t.Publisher {
			t.AudioTrack = v.GetAudioTrack()
			t.VideoTrack = v.GetVideoTrack()
		}
	case SEKick, SEclose:
		// close(t.PESChan)
		t.Publisher.OnEvent(event)
	default:
		t.Publisher.OnEvent(event)
	}
}

func (t *TSPublisher) OnPmtStream(s mpegts.MpegTsPmtStream) {
	switch s.StreamType {
	case mpegts.STREAM_TYPE_H264:
		if t.VideoTrack == nil {
			t.VideoTrack = track.NewH264(t, t.pool)
		}
	case mpegts.STREAM_TYPE_H265:
		if t.VideoTrack == nil {
			t.VideoTrack = track.NewH265(t, t.pool)
		}
	case mpegts.STREAM_TYPE_AAC:
		if t.AudioTrack == nil {
			t.AudioTrack = track.NewAAC(t, t.pool)
		}
	case mpegts.STREAM_TYPE_G711A:
		if t.AudioTrack == nil {
			t.AudioTrack = track.NewG711(t, true, t.pool)
		}
	case mpegts.STREAM_TYPE_G711U:
		if t.AudioTrack == nil {
			t.AudioTrack = track.NewG711(t, false, t.pool)
		}
	default:
		t.Warn("unsupport stream type:", zap.Uint8("type", s.StreamType))
	}
}

func (t *TSReader) Close() {
	close(t.PESChan)
}

func (t *TSReader) ReadPES() {
	for pes := range t.PESChan {
		if t.Err() != nil {
			continue
		}
		if pes.Header.Dts == 0 {
			pes.Header.Dts = pes.Header.Pts
		}
		switch pes.Header.StreamID & 0xF0 {
		case mpegts.STREAM_ID_VIDEO:
			if t.VideoTrack == nil {
				for _, s := range t.PMT.Stream {
					t.OnPmtStream(s)
				}
			}
			if t.VideoTrack != nil {
				t.WriteAnnexB(uint32(pes.Header.Pts), uint32(pes.Header.Dts), pes.Payload)
			}
		default:
			if t.AudioTrack == nil {
				for _, s := range t.PMT.Stream {
					t.OnPmtStream(s)
				}
			}
			if t.AudioTrack != nil {
				switch t.AudioTrack.(type) {
				case *track.AAC:
					t.AudioTrack.WriteADTS(uint32(pes.Header.Pts), pes.Payload)
				case *track.G711:
					t.AudioTrack.WriteRawBytes(uint32(pes.Header.Pts), pes.Payload)
				}
			}
		}
	}
}
