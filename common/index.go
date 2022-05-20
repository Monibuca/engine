package common

import (
	"time"

	"github.com/pion/rtp"
)

// Base 基础Track类
type Base struct {
	Name   string
	Stream IStream `json:"-"`
	ts     time.Time
	bytes  int
	frames int
	BPS    int
	FPS    int
}

func (bt *Base) ComputeBPS(bytes int) {
	bt.bytes += bytes
	bt.frames++
	if elapse := time.Since(bt.ts).Seconds(); elapse > 1 {
		bt.BPS = bt.bytes / int(elapse)
		bt.FPS = bt.frames / int(elapse)
		bt.bytes = 0
		bt.frames = 0
		bt.ts = time.Now()
	}
}

func (bt *Base) GetBase() *Base {
	return bt
}

func (bt *Base) Flush(bf *BaseFrame) {
	bt.ComputeBPS(bf.BytesIn)
	bf.Timestamp = time.Now()
}

type Track interface {
	GetBase() *Base
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
