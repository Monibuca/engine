package engine

import (
	"context"
	"sync"
	"time"
)

type Track interface {
	GetBPS(int)
	Dispose()
}

type Track_Audio struct {
	Buffer      *Ring_Audio `json:"-"`
	Stream      *Stream     `json:"-"`
	PacketCount int
	CodecID     byte
	BPS         int
	lastIndex   byte
}

func (t *Track_Audio) GetBPS(payloadLen int) {
	t.PacketCount++
	if lastTimestamp := t.Buffer.GetAt(t.lastIndex).Timestamp; lastTimestamp > 0 && lastTimestamp != t.Buffer.Current.Timestamp {
		t.BPS = payloadLen * 1000 / int(t.Buffer.Current.Timestamp-lastTimestamp)
	}
	t.lastIndex = t.Buffer.Index
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
	lastIndex   byte
}

func (t *Track_Video) GetBPS(payloadLen int) {
	t.PacketCount++
	if lastTimestamp := t.Buffer.GetAt(t.lastIndex).Timestamp; lastTimestamp > 0 && lastTimestamp != t.Buffer.Current.Timestamp {
		t.BPS = payloadLen * 1000 / int(t.Buffer.Current.Timestamp-lastTimestamp)
	}
	t.lastIndex = t.Buffer.Index
}
func (t *Track_Video) Dispose() {
	t.Buffer.Dispose()
}

type TrackWaiter struct {
	Track
	*sync.Cond `json:"-"`
}

func (tw *TrackWaiter) Ok(t Track) {
	tw.Track = t
	tw.Broadcast()
}
func (tw *TrackWaiter) Dispose() {
	if tw.Cond != nil {
		tw.Broadcast()
	}
	if tw.Track != nil {
		tw.Track.Dispose()
	}
}
func (tw *TrackWaiter) Wait(c chan<- Track) {
	tw.L.Lock()
	tw.Cond.Wait()
	tw.L.Unlock()
	c <- tw.Track
}

type Tracks struct {
	m map[string]*TrackWaiter
	sync.RWMutex
	context.Context
}

func (ts *Tracks) Codecs() (result []string) {
	ts.RLock()
	defer ts.RUnlock()
	for codec := range ts.m {
		result = append(result, codec)
	}
	return
}

func (ts *Tracks) Init() {
	ts.m = make(map[string]*TrackWaiter)
	ts.Context, _ = context.WithTimeout(context.Background(), time.Second*5)
}

func (ts *Tracks) Dispose() {
	ts.RLock()
	defer ts.RUnlock()
	for _, t := range ts.m {
		t.Dispose()
	}
}
func (ts *Tracks) AddTrack(codec string, t Track) {
	ts.Lock()
	if tw, ok := ts.m[codec]; ok {
		ts.Unlock()
		tw.Ok(t)
	} else {
		ts.m[codec] = &TrackWaiter{Track: t}
		ts.Unlock()
	}
}
func (ts *Tracks) GetTrack(codec string) (tw *TrackWaiter, ok bool) {
	ts.Lock()
	if tw, ok = ts.m[codec]; ok {
		ts.Unlock()
		ok = tw.Track != nil
	} else {
		tw = &TrackWaiter{Cond: sync.NewCond(new(sync.Mutex))}
		ts.m[codec] = tw
		ts.Unlock()
	}
	return
}
func (ts *Tracks) WaitTrack(codecs ...string) Track {
	if len(codecs) == 0 {
		codecs = ts.Codecs()
	}
	var tws []*TrackWaiter
	for _, codec := range codecs {
		if tw, ok := ts.GetTrack(codec); ok {
			return tw.Track
		} else {
			tws = append(tws, tw)
		}
	}
	if ts.Err() != nil {
		return nil
	}
	c := make(chan Track, len(tws))
	for _, tw := range tws {
		go tw.Wait(c)
	}
	select {
	case <-ts.Done():
		return nil
	case t := <-c:
		return t
	}
}
