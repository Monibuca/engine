package engine

import (
	"go.uber.org/zap"
	"m7s.live/engine/v4/codec/mpegts"
	"m7s.live/engine/v4/common"
	"m7s.live/engine/v4/track"
)

type TSPublisher struct {
	Publisher
	mpegts.MpegTsStream
	adts []byte
}

func (t *TSPublisher) OnEvent(event any) {
	switch v := event.(type) {
	case IPublisher:
		t.PESChan = make(chan *mpegts.MpegTsPESPacket, 50)
		go t.ReadPES()
		if !t.Equal(v) {
			t.AudioTrack = v.getAudioTrack()
			t.VideoTrack = v.getVideoTrack()
		}
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
			t.AudioTrack = track.NewAAC(t.Publisher.Stream, uint32(90000))
		}
	case mpegts.STREAM_TYPE_G711A:
		if t.AudioTrack == nil {
			t.AudioTrack = track.NewG711(t.Publisher.Stream, true, uint32(90000))
		}
	case mpegts.STREAM_TYPE_G711U:
		if t.AudioTrack == nil {
			t.AudioTrack = track.NewG711(t.Publisher.Stream, false, uint32(90000))
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
		case mpegts.STREAM_ID_AUDIO:
			if t.AudioTrack == nil {
				for _, s := range t.PMT.Stream {
					t.OnPmtStream(s)
				}
			}
			if t.AudioTrack != nil {
				switch t.AudioTrack.(type) {
				case *track.AAC:
					if t.adts == nil {
						t.adts = append(t.adts, pes.Payload[:7]...)
						t.AudioTrack.WriteADTS(t.adts)
					}
					remainLen := len(pes.Payload)
					for remainLen > 0 {
						// AACFrameLength(13)
						// xx xxxxxxxx xxx
						frameLen := (int(pes.Payload[3]&3) << 11) | (int(pes.Payload[4]) << 3) | (int(pes.Payload[5]) >> 5)
						if frameLen > remainLen {
							break
						}
						t.AudioTrack.WriteRaw(uint32(pes.Header.Pts), pes.Payload[:frameLen])
						pes.Payload = pes.Payload[frameLen:remainLen]
						remainLen -= frameLen
					}
				case *track.G711:
					t.AudioTrack.WriteRaw(uint32(pes.Header.Pts), pes.Payload)
				}
			}
		case mpegts.STREAM_ID_VIDEO:
			if t.VideoTrack == nil {
				for _, s := range t.PMT.Stream {
					t.OnPmtStream(s)
				}
			}
			if t.VideoTrack != nil {
				t.VideoTrack.WriteAnnexB(uint32(pes.Header.Pts), uint32(pes.Header.Dts), common.AnnexBFrame(pes.Payload))
			}
		}
	}
}
