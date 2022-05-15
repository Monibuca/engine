package common

import (
	"time"

	"github.com/pion/rtp"
)

type Track interface {
	GetName() string
	LastWriteTime() time.Time
}

type AVTrack interface {
	Track
	Attach()
	Detach()
	WriteAVCC(ts uint32, frame AVCCFrame) //写入AVCC格式的数据
	WriteRTP([]byte)
	WriteRTPPack(*rtp.Packet)
	Flush()
}
type VideoTrack interface {
	AVTrack
	GetDecoderConfiguration() DecoderConfiguration[NALUSlice]
	CurrentFrame() *AVFrame[NALUSlice]
	PreFrame() *AVFrame[NALUSlice]
	WriteSlice(NALUSlice)
	WriteAnnexB(uint32, uint32, AnnexBFrame)
}

type AudioTrack interface {
	AVTrack
	GetDecoderConfiguration() DecoderConfiguration[AudioSlice]
	CurrentFrame() *AVFrame[AudioSlice]
	PreFrame() *AVFrame[AudioSlice]
	WriteSlice(AudioSlice)
	WriteADTS([]byte)
}

type BPS struct {
	ts    time.Time
	bytes int
	BPS   int
}

func (bps *BPS) ComputeBPS(bytes int) {
	bps.bytes += bytes
	if elapse := time.Since(bps.ts).Seconds(); elapse > 1 {
		bps.BPS = bps.bytes / int(elapse)
		bps.ts = time.Now()
	}
}
