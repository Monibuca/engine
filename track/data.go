package track

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
	"m7s.live/engine/v4/common"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/util"
)

type Data struct {
	Base
	LockRing[any]
	sync.Locker // 写入锁，可选，单一协程写入可以不加锁
}

func (d *Data) ReadRing() *LockRing[any] {
	return util.Clone(d.LockRing)
}

func (d *Data) LastWriteTime() time.Time {
	return d.LockRing.RingBuffer.LastValue.WriteTime
}

func (dt *Data) Push(data any) {
	if dt.Locker != nil {
		dt.Lock()
		defer dt.Unlock()
	}
	dt.Write(data)
}

func (d *Data) Play(ctx context.Context, onData func(any) error) error {
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

func (d *Data) Attach() {
	promise := util.NewPromise(common.Track(d))
	d.Stream.AddTrack(promise)
	if err := promise.Await(); err != nil {
		d.Error("attach data track failed", zap.Error(err))
	} else {
		d.Info("data track attached")
	}
}
