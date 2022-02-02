package engine

// import (
// 	"sync"
// 	"time"
// 	"unsafe"

// 	"github.com/Monibuca/engine/v4/util"
// )

// type DataTrack struct {
// 	LockRing[any]
// 	BaseTrack
// 	*LockFrame[any]
// 	sync.Locker // 写入锁，可选，单一写入可以不加锁
// }

// func (s *Stream) NewDataTrack(l sync.Locker) (dt *DataTrack) {
// 	dt = &DataTrack{
// 		Locker: l,
// 	}
// 	dt.Stream = s
// 	dt.Init(s.Context, 256)
// 	dt.setCurrent()
// 	return
// }

// func (dt *DataTrack) Push(data any) {
// 	if dt.Locker != nil {
// 		dt.Lock()
// 		defer dt.Unlock()
// 	}
// 	dt.Timestamp = time.Now()
// 	dt.bytesIn = (int(unsafe.Sizeof(data)))
// 	dt.GetBPS()
// 	dt.Write(data)
// 	dt.setCurrent()
// }

// func (at *DataTrack) setCurrent() {
// 	at.LockFrame = at.Current()
// }

// func (dt *DataTrack) Play(onData func(DataFrame[any]), exit1, exit2 <-chan struct{}) {
// 	dr := util.Clone(dt.LockRing)
// 	for dp := dr.Read(); ; dp = dr.Read() {
// 		select {
// 		case <-exit1:
// 			return
// 		case <-exit2:
// 			return
// 		default:
// 			onData(dp)
// 			dr.MoveNext()
// 		}
// 	}
// }
