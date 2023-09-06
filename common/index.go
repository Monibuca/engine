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
type Base[T any, F IDataFrame[T]] struct {
	RingWriter[T, F]
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

func (bt *Base[T, F]) ComputeBPS(bytes int) {
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

func (bt *Base[T, F]) GetName() string {
	return bt.Name
}

func (bt *Base[T, F]) GetBPS() int {
	return bt.BPS
}

func (bt *Base[T, F]) GetFPS() int {
	return bt.FPS
}

func (bt *Base[T, F]) GetDrops() int {
	return bt.Drops
}

// GetRBSize 获取缓冲区大小
func (bt *Base[T, F]) GetRBSize() int {
	return bt.RingWriter.Size
}

func (bt *Base[T, F]) SnapForJson() {
}

func (bt *Base[T, F]) SetStuff(stuff ...any) {
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

func (bt *Base[T, F]) Dispose() {
	bt.Value.Broadcast()
}

type Track interface {
	GetName() string
	GetBPS() int
	GetFPS() int
	GetDrops() int
	LastWriteTime() time.Time
	SnapForJson()
	SetStuff(stuff ...any)
	GetRBSize() int
	Dispose()
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
