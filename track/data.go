package track

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/util"
)

type Custom interface {
	Track
	Dispose()
}

type Data[T any] struct {
	Base
	LockRing[T]
	sync.Locker // 写入锁，可选，单一协程写入可以不加锁
}

func (d *Data[T]) GetRBSize() int {
	return d.LockRing.RingBuffer.Size
}

func (d *Data[T]) ReadRing() *LockRing[T] {
	return util.Clone(d.LockRing)
}

func (d *Data[T]) LastWriteTime() time.Time {
	return d.LockRing.RingBuffer.LastValue.WriteTime
}

func (dt *Data[T]) Push(data T) {
	if dt.Locker != nil {
		dt.Lock()
		defer dt.Unlock()
	}
	dt.Value.WriteTime = time.Now()
	dt.Write(data)
}

func (d *Data[T]) Play(ctx context.Context, onData func(*DataFrame[T]) error) error {
	for r := d.ReadRing(); ctx.Err() == nil; r.MoveNext() {
		p := r.Read()
		if *r.Flag == 2 {
			break
		}
		if err := onData(p); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (d *Data[T]) Attach(s IStream) {
	if err := s.AddTrack(d).Await(); err != nil {
		d.Error("attach data track failed", zap.Error(err))
	} else {
		d.Info("data track attached")
	}
}

func NewDataTrack[T any](name string) (dt *Data[T]) {
	dt = &Data[T]{}
	dt.Init(10)
	dt.SetStuff(name)
	return
}
