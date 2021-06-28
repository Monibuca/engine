package engine

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

type Track interface {
	GetBPS()
	Dispose()
}

type Track_Audio struct {
	Buffer      *Ring_Audio `json:"-"`
	Stream      *Stream     `json:"-"`
	PacketCount int
	CodecID     byte
	BPS         int
	bytes       int    // GOP内的数据大小
	ts          uint32 // GOP起始时间戳
}

func (t *Track_Audio) GetBPS() {
	t.PacketCount++
	if t.Buffer.Current.Timestamp != t.ts {
		t.BPS = t.bytes * 1000 / int(t.Buffer.Current.Timestamp-t.ts)
	}
}
func (t *Track_Audio) Dispose() {
	t.Buffer.Dispose()
}

type Track_Video struct {
	Buffer      *Ring_Video `json:"-"`
	Stream      *Stream     `json:"-"`
	PacketCount int
	CodecID     byte
	BPS         int
	bytes       int    // GOP内的数据大小
	ts          uint32 // GOP起始时间戳
}

func (t *Track_Video) GetBPS() {
	t.PacketCount++
	if t.Buffer.Current.Timestamp != t.ts {
		t.BPS = t.bytes * 1000 / int(t.Buffer.Current.Timestamp-t.ts)
	}
}
func (t *Track_Video) Dispose() {
	t.Buffer.Dispose()
}

type Tracks struct {
	TrackRing *Ring_Track
	m         map[string]Track
	context.Context
	sync.RWMutex
}

func (ts *Tracks) MarshalJSON() ([]byte, error) {
	ts.RLock()
	defer ts.RUnlock()
	return json.Marshal(ts.m)
}

func (ts *Tracks) Init() {
	ts.TrackRing = NewRing_Track()
	ts.m = make(map[string]Track)
	ts.Context, _ = context.WithTimeout(context.Background(), time.Second*5)
}

func (ts *Tracks) Dispose() {
	var i byte
	for i = 0; i < ts.TrackRing.Index; i++ {
		ts.TrackRing.GetAt(i).Dispose()
	}
	ts.TrackRing.disposed = true
	ts.TrackRing.Current.Done()
}
func (ts *Tracks) AddTrack(codec string, t Track) {
	ts.Lock()
	defer ts.Unlock()
	if _, ok := ts.m[codec]; !ok {
		ts.m[codec] = t
		ts.TrackRing.NextW(codec, t)
	}
}

func (ts *Tracks) WaitTrack(codecs ...string) Track {
	ring := ts.TrackRing.SubRing(0)
	if ts.Context.Err() == nil { //在等待时间范围内
		wait := make(chan *RingItem_Track)
		if len(codecs) == 0 { //任意编码需求，只取第一个
			go func() {
				ring.Current.Wait()
				wait <- ring.Current
			}()
		} else {
			go func() {
				for ring.Current.Wait(); !ts.TrackRing.disposed; ring.NextR() {
					wait <- ring.Current
				}
			}()
		}
		select {
		case t := <-wait:
			return t.Track
		case <-ts.Context.Done():
			return nil
		}
	} else { //进入不等待状态
		if len(codecs) == 0 {
			return ring.Current.Track
		} else {
			for ; ring.Index < ts.TrackRing.Index; ring.GoNext() {
				for _, codec := range codecs {
					if ring.Current.Codec == codec {
						return ring.Current.Track
					}
				}
			}
			return nil
		}
	}
}

type RingItem_Track struct {
	Track
	Codec string
	sync.WaitGroup
}

// Ring 环形缓冲，使用数组实现
type Ring_Track struct {
	Current  *RingItem_Track
	buffer   []RingItem_Track
	Index    byte
	disposed bool
}

func (r *Ring_Track) SubRing(index byte) *Ring_Track {
	result := &Ring_Track{
		buffer: r.buffer,
	}
	result.GoTo(index)
	return result
}

// NewRing 创建Ring
func NewRing_Track() (r *Ring_Track) {
	buffer := make([]RingItem_Track, 256)
	r = &Ring_Track{
		buffer:  buffer,
		Current: &buffer[0],
	}
	r.Current.Add(1)
	return
}
func (r *Ring_Track) GetAt(index byte) *RingItem_Track {
	return &r.buffer[index]
}

// GoTo 移动到指定索引处
func (r *Ring_Track) GoTo(index byte) {
	r.Index = index
	r.Current = &r.buffer[index]
}

// GoNext 移动到下一个位置
func (r *Ring_Track) GoNext() {
	r.Index = r.Index + 1
	r.Current = &r.buffer[r.Index]
}

// NextW 写下一个
func (r *Ring_Track) NextW(codec string, track Track) {
	item := r.Current
	item.Track = track
	r.GoNext()
	r.Current.Add(1)
	item.Done()
}

// NextR 读下一个
func (r *Ring_Track) NextR() {
	r.Current.Wait()
	r.GoNext()
}
