package engine

import (
	"sync"
	"time"
	"unsafe"
)

type DataTrack struct {
	RingBuffer
	BaseTrack
	*LockItem
	sync.Locker // 写入锁，可选，单一写入可以不加锁
}

func (s *Stream) NewDataTrack(l sync.Locker) (dt *DataTrack) {
	dt = &DataTrack{
		Locker: l,
	}
	dt.Stream = s
	dt.Init(s.Context, 256)
	dt.setCurrent()
	return
}

func (dt *DataTrack) Push(data interface{}) {
	if dt.Locker != nil {
		dt.Lock()
		defer dt.Unlock()
	}
	dt.Timestamp = time.Now()
	dt.addBytes(int(unsafe.Sizeof(data)))
	dt.GetBPS()
	if time.Since(dt.ts) > 1000 {
		dt.resetBPS()
	}
	dt.Write(data)
	dt.setCurrent()
}

func (at *DataTrack) setCurrent() {
	at.LockItem = at.Current()
}

func (dt *DataTrack) Play(onData func(*DataItem), exit1, exit2 <-chan struct{}) {
	dr := dt.Clone()
	for dp := dr.Read(); ; dp = dr.Read() {
		select {
		case <-exit1:
			return
		case <-exit2:
			return
		default:
			onData(dp.(*DataItem))
			dr.MoveNext()
		}
	}
}
