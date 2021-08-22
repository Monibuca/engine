package engine

import (
	"bytes"
	"container/ring"
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/Monibuca/utils/v3"
)

type Track interface {
	GetBPS()
}
type BaseTrack struct {
	Stream      *Stream `json:"-"`
	PacketCount int
	BPS         int
	bytes       int
	ts          time.Time
}

func (t *BaseTrack) addBytes(size int) {
	t.bytes += size
}

type AVPack struct {
	bytes.Buffer
	Payload []byte
}

func (pack *AVPack) Bytes2Payload() {
	pack.Payload = pack.Bytes()
}

type AVTrack struct {
	AVRing  `json:"-"`
	CodecID byte
	BaseTrack
	*AVItem  `json:"-"` //当前正在写入的数据对象
	lastTs   uint32
	lastTime time.Time
	timebase time.Duration
}

func (t *DataTrack) resetBPS() {
	t.bytes = 0
	t.ts = t.Current().Timestamp
}

func (t *DataTrack) GetBPS() {
	t.PacketCount++
	t.Sequence = t.PacketCount
	if delta := time.Since(t.ts); delta != 0 {
		t.BPS = t.bytes * 1000 / int(delta)
	}
}

func (t *AVTrack) setCurrent() {
	t.AVItem = t.Current()
}

func (t *AVTrack) resetBPS() {
	t.bytes = 0
	t.ts = t.Current().Timestamp
}

func (t *AVTrack) GetBPS() {
	t.PacketCount++
	t.Sequence = t.PacketCount
	if delta := int(t.Timestamp.Sub(t.ts).Seconds()); delta != 0 {
		t.BPS = t.bytes / delta
	}
}

func (t *AVTrack) setTS(ts uint32) {
	if t.lastTs == 0 {
		t.Timestamp = time.Now()
	} else {
		if t.lastTs > ts || ts-t.lastTs > 10000 {
			utils.Printf("timestamp wrong %s lastTs:%d currentTs:%d", t.Stream.StreamPath, t.lastTs, ts)
			//按照频率估算时间戳增量
			t.Timestamp = t.lastTime.Add(time.Second / t.timebase)
		} else {
			t.Timestamp = t.lastTime.Add(time.Duration(ts-t.lastTs) * time.Millisecond)
		}
	}
	t.lastTs = ts
	t.lastTime = t.Timestamp
}

// func (t *Track_Base) Dispose() {
// 	t.RingDisposable.Dispose()
// }

type Tracks struct {
	RingBuffer
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

func (ts *Tracks) Init(ctx context.Context) {
	ts.RingBuffer.Init(ctx, 8)
	ts.head = ts.Ring
	ts.m = make(map[string]Track)
	ts.Context, _ = context.WithTimeout(context.Background(), time.Second*5)
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
