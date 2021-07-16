package engine

import (
	"bytes"
	"container/ring"
	"context"
	"encoding/json"
	"sync"
	"time"
)

type Track interface {
	GetBPS()
	Dispose()
}

type AVPack interface {
	Since(uint32) uint32
}

type BasePack struct {
	Timestamp uint32
	Sequence  int
	*bytes.Buffer
	Payload []byte
}

func (p *BasePack) Since(ts uint32) uint32 {
	return p.Timestamp - ts
}

type Track_Base struct {
	RingDisposable `json:"-"`
	Stream         *Stream `json:"-"`
	PacketCount    int
	CodecID        byte
	BPS            int
	bytes          int    // GOP内的数据大小
	ts             uint32 // GOP起始时间戳
	lastTs         uint32 //最新的时间戳
}

func (t *Track_Base) GetBPS() {
	avPack := t.CurrentValue().(AVPack)
	t.PacketCount++
	if delta := avPack.Since(t.ts); delta != 0 {
		t.BPS = t.bytes * 1000 / int(delta)
	}
}

// func (t *Track_Base) Dispose() {
// 	t.RingDisposable.Dispose()
// }

type Tracks struct {
	RingDisposable
	m map[string]Track
	context.Context
	sync.RWMutex
	head *ring.Ring
}

func (ts *Tracks) MarshalJSON() ([]byte, error) {
	ts.RLock()
	defer ts.RUnlock()
	return json.Marshal(ts.m)
}

func (ts *Tracks) Init() {
	ts.RingDisposable.Init(8)
	ts.head = ts.Ring
	ts.m = make(map[string]Track)
	ts.Context, _ = context.WithTimeout(context.Background(), time.Second*5)
}

func (ts *Tracks) Dispose() {
	ts.RLock()
	for _, v := range ts.m {
		v.Dispose()
	}
	ts.RUnlock()
	ts.RingDisposable.Dispose()
}
func (ts *Tracks) AddTrack(name string, t Track) {
	ts.Lock()
	defer ts.Unlock()
	if _, ok := ts.m[name]; !ok {
		ts.m[name] = t
		ts.Write(name)
	}
}
func (ts *Tracks) GetTrack(name string) Track {
	ts.RLock()
	defer ts.RUnlock()
	return ts.m[name]
}

func (ts *Tracks) OnTrack(callback func(string, Track)) {
	ts.SubRing(ts.head).ReadLoop(func(name string) {
		callback(name, ts.GetTrack(name))
	})
}

func (ts *Tracks) WaitTrack(names ...string) Track {
	ring := ts.SubRing(ts.head)
	if ts.Context.Err() == nil { //在等待时间范围内
		if wait := make(chan string); len(names) == 0 { //任意编码需求，只取第一个
			go func() {
				if rt, ok := ring.Read().(string); ok {
					wait <- rt
				}
			}()
			select {
			case t := <-wait:
				return ts.GetTrack(t)
			case <-ts.Context.Done():
				return nil
			}
		} else {
			go ring.ReadLoop(wait)
			// go func() {
			// 	for {
			// 		if rt, ok := ring.Read().(string); ok {
			// 			wait <- rt
			// 			ring.MoveNext()
			// 		} else {
			// 			break
			// 		}
			// 	}
			// }()
			for {
				select {
				case t := <-wait:
					for _, name := range names {
						if t == name {
							return ts.GetTrack(t)
						}
					}
				case <-ts.Context.Done():
					return nil
				}
			}
		}
	} else { //进入不等待状态
		ts.RLock()
		defer ts.RUnlock()
		if len(names) == 0 {
			if len(ts.m) == 0 {
				return nil
			}
			return ts.m[ring.Read().(string)]
		} else {
			for _, name := range names {
				if t, ok := ts.m[name]; ok {
					return t
				}
			}
			return nil
		}
	}
}
