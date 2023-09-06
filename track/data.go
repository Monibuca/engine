package track

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/log"
	"m7s.live/engine/v4/util"
)

type Data[T any] struct {
	Base[T, *DataFrame[T]]
	sync.Locker `json:"-" yaml:"-"` // 写入锁，可选，单一协程写入可以不加锁
}

func (dt *Data[T]) Init(n int) {
	dt.Base.Init(n, NewDataFrame[T])
}

func (dt *Data[T]) Push(data T) {
	if dt.Locker != nil {
		dt.Lock()
		defer dt.Unlock()
	}
	curValue := dt.Value
	if log.Trace {
		dt.Trace("push data", zap.Uint32("sequence", curValue.Sequence))
	}
	curValue.Data = data
	dt.Step()
}

func (d *Data[T]) Play(ctx context.Context, onData func(*DataFrame[T]) error) (err error) {
	d.Debug("play data track")
	reader := DataReader[T]{}
	for err = reader.StartRead(d.Ring); err == nil; err = reader.ReadNext() {
		if log.Trace {
			d.Trace("read data", zap.Uint32("sequence", reader.Value.Sequence))
		}
		if err = onData(reader.Value); err == nil {
			err = ctx.Err()
		}
		if err != nil {
			reader.Value.ReaderLeave()
			return
		}
	}
	return
}

func (d *Data[T]) Attach(s IStream) {
	d.SetStuff(s)
	if err := s.AddTrack(d).Await(); err != nil {
		d.Error("attach data track failed", zap.Error(err))
	} else {
		d.Info("data track attached")
	}
}

func (d *Data[T]) LastWriteTime() time.Time {
	return d.LastValue.WriteTime
}

func NewDataTrack[T any](name string) (dt *Data[T]) {
	dt = &Data[T]{}
	dt.Init(10)
	dt.SetStuff(name)
	return
}

type RecycleData[T util.Recyclable] struct {
	Data[T]
}

func (dt *RecycleData[T]) Push(data T) {
	if dt.Locker != nil {
		dt.Lock()
		defer dt.Unlock()
	}
	curValue := dt.Value
	if log.Trace {
		dt.Trace("push data", zap.Uint32("sequence", curValue.Sequence))
	}
	curValue.Data = data
	dt.Step()
	if !dt.Value.WriteTime.IsZero() {
		dt.Value.Data.Recycle()
	}
}

func NewRecycleDataTrack[T util.Recyclable](name string) (dt *RecycleData[T]) {
	dt = &RecycleData[T]{}
	dt.Init(10)
	dt.SetStuff(name)
	return
}

type BytesData struct {
	RecycleData[*util.ListItem[util.Buffer]]
	Pool util.BytesPool
}

func NewBytesDataTrack(name string) (dt *BytesData) {
	dt = &BytesData{
		Pool: make(util.BytesPool, 17),
	}
	dt.Init(10)
	dt.SetStuff(name)
	return
}
