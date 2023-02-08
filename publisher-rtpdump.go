package engine

import (
	"os"
	"time"

	"github.com/pion/webrtc/v3/pkg/media/rtpdump"
	"go.uber.org/zap"
	"m7s.live/engine/v4/codec"
	"m7s.live/engine/v4/track"
)

type RTPDumpPublisher struct {
	Publisher
	DumpFile string
	VCodec   codec.VideoCodecID
	ACodec   codec.AudioCodecID
	file     *os.File
}

func (t *RTPDumpPublisher) OnEvent(event any) {
	var err error
	t.Publisher.OnEvent(event)
	switch event.(type) {
	case IPublisher:
		t.file, err = os.Open(t.DumpFile)
		if err != nil {
			t.Stream.Error("RTPDumpPublisher open file error", zap.Error(err))
			return
		}
		r, h, err := rtpdump.NewReader(t.file)
		if err != nil {
			t.Stream.Error("RTPDumpPublisher open file error", zap.Error(err))
			return
		}
		t.Stream.Info("RTPDumpPublisher open file success", zap.String("file", t.DumpFile), zap.String("start", h.Start.String()), zap.String("source", h.Source.String()), zap.Uint16("port", h.Port))
		switch t.VCodec {
		case codec.CodecID_H264:
			t.VideoTrack = track.NewH264(t.Publisher.Stream)
		case codec.CodecID_H265:
			t.VideoTrack = track.NewH265(t.Publisher.Stream)
		}
		switch t.ACodec {
		case codec.CodecID_AAC:
			t.AudioTrack = track.NewAAC(t.Publisher.Stream)
		case codec.CodecID_PCMA:
			t.AudioTrack = track.NewG711(t.Publisher.Stream, true)
		case codec.CodecID_PCMU:
			t.AudioTrack = track.NewG711(t.Publisher.Stream, false)
		}
		t.VideoTrack.SetSpeedLimit(500 * time.Millisecond)
		t.AudioTrack.SetSpeedLimit(500 * time.Millisecond)
		for {
			packet, err := r.Next()
			if err != nil {
				t.Stream.Error("RTPDumpPublisher read file error", zap.Error(err))
				return
			}
			if !packet.IsRTCP {
				t.VideoTrack.WriteRTP(packet.Payload)
			}
			// t.AudioTrack.WriteRTP(packet)
		}
	}
}
