package common

import (
	"time"

	"github.com/pion/rtp"
)

type TimelineData[T any] struct {
	Timestamp time.Time
	Value     T
}

// Base 基础Track类
type Base struct {
	Name     string
	Stream   IStream `json:"-"`
	Attached byte // 0代表准备好后自动attach，1代表已经attach，2代表已经detach
	ts       time.Time
	bytes    int
	frames   int
	BPS      int
	FPS      int
	RawPart  []int               // 裸数据片段用于UI上显示
	RawSize  int                 // 裸数据长度
	BPSs     []TimelineData[int] // 10s码率统计
	FPSs     []TimelineData[int] // 10s帧率统计
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
		bt.BPSs = append(bt.BPSs, TimelineData[int]{Timestamp: bt.ts, Value: bt.BPS})
		if len(bt.BPSs) > 10 {
			copy(bt.BPSs, bt.BPSs[1:])
			bt.BPSs = bt.BPSs[:10]
		}
		bt.FPSs = append(bt.FPSs, TimelineData[int]{Timestamp: bt.ts, Value: bt.FPS})
		if len(bt.FPSs) > 10 {
			copy(bt.FPSs, bt.FPSs[1:])
			bt.FPSs = bt.FPSs[:10]
		}
	}
}

func (bt *Base) GetBase() *Base {
	return bt
}
func (bt *Base) SnapForJson() {
}
func (bt *Base) Flush(bf *BaseFrame) {
	bt.ComputeBPS(bf.BytesIn)
	bf.Timestamp = time.Now()
}

type Track interface {
	GetBase() *Base
	LastWriteTime() time.Time
	SnapForJson()
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
