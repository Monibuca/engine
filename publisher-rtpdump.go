package engine

import (
	"os"
	"sync"
	"time"

	"github.com/bluenviron/mediacommon/pkg/codecs/mpeg4audio"
	"github.com/pion/webrtc/v3/pkg/media/rtpdump"
	"go.uber.org/zap"
	"m7s.live/engine/v4/codec"
	"m7s.live/engine/v4/common"
	"m7s.live/engine/v4/track"
	"m7s.live/engine/v4/util"
)

type RTPDumpPublisher struct {
	Publisher
	VCodec       codec.VideoCodecID
	ACodec       codec.AudioCodecID
	VPayloadType uint8
	APayloadType uint8
	other        rtpdump.Packet
	sync.Mutex
}

func (t *RTPDumpPublisher) Feed(file *os.File) {

	r, h, err := rtpdump.NewReader(file)
	if err != nil {
		t.Stream.Error("RTPDumpPublisher open file error", zap.Error(err))
		return
	}
	t.Lock()
	t.Stream.Info("RTPDumpPublisher open file success", zap.String("file", file.Name()), zap.String("start", h.Start.String()), zap.String("source", h.Source.String()), zap.Uint16("port", h.Port))
	if t.VideoTrack == nil {
		switch t.VCodec {
		case codec.CodecID_H264:
			t.VideoTrack = track.NewH264(t.Publisher.Stream, t.VPayloadType)
		case codec.CodecID_H265:
			t.VideoTrack = track.NewH265(t.Publisher.Stream, t.VPayloadType)
		}
		if t.VideoTrack != nil {
			t.VideoTrack.SetSpeedLimit(500 * time.Millisecond)
		}
	}
	if t.AudioTrack == nil {
		switch t.ACodec {
		case codec.CodecID_AAC:
			at := track.NewAAC(t.Publisher.Stream, t.APayloadType)
			t.AudioTrack = at
			var c mpeg4audio.Config
			c.ChannelCount = 2
			c.SampleRate = 48000
			asc, _ := c.Marshal()
			at.WriteSequenceHead(append([]byte{0xAF, 0x00}, asc...))
		case codec.CodecID_PCMA:
			t.AudioTrack = track.NewG711(t.Publisher.Stream, true, t.APayloadType)
		case codec.CodecID_PCMU:
			t.AudioTrack = track.NewG711(t.Publisher.Stream, false, t.APayloadType)
		}
		if t.AudioTrack != nil {
			t.AudioTrack.SetSpeedLimit(500 * time.Millisecond)
		}
	}
	t.Unlock()
	needLock := true
	for {
		packet, err := r.Next()
		if err != nil {
			t.Stream.Error("RTPDumpPublisher read file error", zap.Error(err))
			return
		}
		if packet.IsRTCP {
			continue
		}
		if needLock {
			t.Lock()
		}
		if t.other.Payload == nil {
			t.other = packet
			t.Unlock()
			needLock = true
			continue
		}
		if packet.Offset >= t.other.Offset {
			t.WriteRTP(t.other.Payload)
			t.other = packet
			t.Unlock()
			needLock = true
			continue
		}
		needLock = false
		t.WriteRTP(packet.Payload)
	}
}
func (t *RTPDumpPublisher) WriteRTP(raw []byte) {
	var frame common.RTPFrame
	frame.Unmarshal(raw)
	switch frame.PayloadType {
	case t.VPayloadType:
		t.VideoTrack.WriteRTP(&util.ListItem[common.RTPFrame]{Value: frame})
	case t.APayloadType:
		t.AudioTrack.WriteRTP(&util.ListItem[common.RTPFrame]{Value: frame})
	default:
		t.Stream.Warn("RTPDumpPublisher unknown payload type", zap.Uint8("payloadType", frame.PayloadType))
	}
}
