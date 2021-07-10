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
func (ts *Tracks) AddTrack(codec string, t Track) {
	ts.Lock()
	defer ts.Unlock()
	if _, ok := ts.m[codec]; !ok {
		ts.m[codec] = t
		ts.Write(codec)
	}
}

func (ts *Tracks) WaitTrack(codecs ...string) Track {
	ring := ts.SubRing(ts.head)
	if ts.Context.Err() == nil { //在等待时间范围内
		wait := make(chan string)
		if len(codecs) == 0 { //任意编码需求，只取第一个
			go func() {
				if rt, ok := ring.Read().(string); ok {
					wait <- rt
				}
			}()
			select {
			case t := <-wait:
				ts.RLock()
				defer ts.RUnlock()
				return ts.m[t]
			case <-ts.Context.Done():
				return nil
			}
		} else {
			go func() {
				for {
					if rt, ok := ring.Read().(string); ok {
						wait <- rt
						ring.MoveNext()
					} else {
						break
					}
				}
			}()
			for {
				select {
				case t := <-wait:
					for _, codec := range codecs {
						if t == codec {
							ts.RLock()
							defer ts.RUnlock()
							return ts.m[t]
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
		if len(codecs) == 0 {
			return ts.m[ring.Read().(string)]
		} else {
			for _, codec := range codecs {
				if t, ok := ts.m[codec]; ok {
					return t
				}
			}
			return nil
		}
	}
}
