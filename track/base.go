package track

import (
	"time"

	"go.uber.org/zap"
	"m7s.live/engine/v4/common"
	"m7s.live/engine/v4/log"
	"m7s.live/engine/v4/util"
)

// Base 基础Track类
type Base[T any, F util.IDataFrame[T]] struct {
	util.RingWriter[T, F]
	Name      string
	log.Zap   `json:"-" yaml:"-"`
	Publisher common.IPuber `json:"-" yaml:"-"` //所属发布者
	State     common.TrackState
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
		case common.IPuber:
			bt.Publisher = v
			bt.Zap = v.With(zap.String("track", bt.Name))
		case common.TrackState:
			bt.State = v
		case string:
			bt.Name = v
		}
	}
}

func (bt *Base[T, F]) GetPublisher() common.IPuber {
	return bt.Publisher
}

func (bt *Base[T, F]) Dispose() {
	bt.Value.Broadcast()
}
