package common

import (
	"sync/atomic"
	"time"

	"github.com/pion/rtp"
	"go.uber.org/zap"
	"m7s.live/engine/v4/log"
	"m7s.live/engine/v4/util"
)

type TimelineData[T any] struct {
	Timestamp time.Time
	Value     T
}
type TrackState byte

const (
	TrackStateOnline  TrackState = iota // 上线
	TrackStateOffline                   // 下线
)

// Base 基础Track类
type Base struct {
	Name      string
	log.Zap   `json:"-" yaml:"-"`
	Stream    IStream     `json:"-" yaml:"-"`
	Attached  atomic.Bool `json:"-" yaml:"-"`
	State     TrackState
	ts        time.Time
	bytes     int
	frames    int
	DropCount int `json:"-" yaml:"-"` //丢帧数
	BPS       int
	FPS       int
	Drops     int   // 丢帧率
	RawSize   int   // 裸数据长度
	RawPart   []int // 裸数据片段用于UI上显示
}

func (bt *Base) ComputeBPS(bytes int) {
	bt.bytes += bytes
	bt.frames++
	if elapse := time.Since(bt.ts).Seconds(); elapse > 1 {
		bt.BPS = int(float64(bt.bytes) / elapse)
		bt.FPS = int(float64(bt.frames) / elapse)
		bt.Drops = int(float64(bt.DropCount) / elapse)
		bt.bytes = 0
		bt.frames = 0
		bt.DropCount = 0
		bt.ts = time.Now()
	}
}

func (bt *Base) GetBase() *Base {
	return bt
}

// GetRBSize 获取缓冲区大小
func (bt *Base) GetRBSize() int {
	return 0
}

func (bt *Base) SnapForJson() {
}
func (bt *Base) Flush(bf *BaseFrame) {
	bt.ComputeBPS(bf.BytesIn)
	bf.WriteTime = time.Now()
}
func (bt *Base) SetStuff(stuff ...any) {
	for _, s := range stuff {
		switch v := s.(type) {
		case IStream:
			bt.Stream = v
			bt.Zap = v.With(zap.String("track", bt.Name))
		case TrackState:
			bt.State = v
		case string:
			bt.Name = v
		}
	}
}

type Track interface {
	GetBase() *Base
	LastWriteTime() time.Time
	SnapForJson()
	SetStuff(stuff ...any)
	GetRBSize() int
}

type AVTrack interface {
	Track
	PreFrame() *AVFrame
	CurrentFrame() *AVFrame
	Attach()
	Detach()
	WriteAVCC(ts uint32, frame *util.BLL) error //写入AVCC格式的数据
	WriteRTP(*util.ListItem[RTPFrame])
	WriteRTPPack(*rtp.Packet)
	Flush()
	SetSpeedLimit(time.Duration)
	GetRTPFromPool() *util.ListItem[RTPFrame]
	GetFromPool(util.IBytes) *util.ListItem[util.Buffer]
}
type VideoTrack interface {
	AVTrack
	WriteSliceBytes(slice []byte)
	WriteNalu(uint32, uint32, []byte)
	WriteAnnexB(uint32, uint32, []byte)
	SetLostFlag()
}

type AudioTrack interface {
	AVTrack
	WriteADTS(uint32, util.IBytes)
	WriteRawBytes(uint32, util.IBytes)
}
