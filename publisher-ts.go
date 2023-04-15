package engine

import (
	"go.uber.org/zap"
	"m7s.live/engine/v4/codec/mpegts"
	"m7s.live/engine/v4/track"
)

type TSPublisher struct {
	Publisher
	mpegts.MpegTsStream `json:"-" yaml:"-"`
}

func (t *TSPublisher) OnEvent(event any) {
	switch v := event.(type) {
	case IPublisher:
		t.PESChan = make(chan *mpegts.MpegTsPESPacket, 50)
		t.PESBuffer = make(map[uint16]*mpegts.MpegTsPESPacket)
		go t.ReadPES()
		if !t.Equal(v) {
			t.AudioTrack = v.getAudioTrack()
			t.VideoTrack = v.getVideoTrack()
		}
	case SEKick, SEclose:
		close(t.PESChan)
		t.Publisher.OnEvent(event)
	default:
		t.Publisher.OnEvent(event)
	}
}

func (t *TSPublisher) OnPmtStream(s mpegts.MpegTsPmtStream) {
	switch s.StreamType {
	case mpegts.STREAM_TYPE_H264:
		if t.VideoTrack == nil {
			t.VideoTrack = track.NewH264(t.Publisher.Stream)
		}
	case mpegts.STREAM_TYPE_H265:
		if t.VideoTrack == nil {
			t.VideoTrack = track.NewH265(t.Publisher.Stream)
		}
	case mpegts.STREAM_TYPE_AAC:
		if t.AudioTrack == nil {
			t.AudioTrack = track.NewAAC(t.Publisher.Stream)
		}
	case mpegts.STREAM_TYPE_G711A:
		if t.AudioTrack == nil {
			t.AudioTrack = track.NewG711(t.Publisher.Stream, true)
		}
	case mpegts.STREAM_TYPE_G711U:
		if t.AudioTrack == nil {
			t.AudioTrack = track.NewG711(t.Publisher.Stream, false)
		}
	default:
		t.Warn("unsupport stream type:", zap.Uint8("type", s.StreamType))
	}
}

func (t *TSPublisher) ReadPES() {
	for pes := range t.PESChan {
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
					t.AudioTrack.WriteRaw(uint32(pes.Header.Pts), pes.Payload)
				}
			}
		}
	}
}
