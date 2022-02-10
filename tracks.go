package engine

import (
	"context"
	"encoding/json"
	"sync"

	. "github.com/Monibuca/engine/v4/common"
)

type Tracks struct {
	context.Context
	sync.RWMutex
	m       map[string]Track
	waiters map[string][]*chan Track
}

func (ts *Tracks) MarshalJSON() ([]byte, error) {
	ts.RLock()
	defer ts.RUnlock()
	return json.Marshal(ts.m)
}

func (ts *Tracks) Init(ctx context.Context) {
	ts.m = make(map[string]Track)
	ts.waiters = make(map[string][]*chan Track)
	ts.Context = ctx
}

func (s *Stream) AddTrack(t Track) {
	s.Tracks.Lock()
	defer s.Tracks.Unlock()
	name := t.GetName()
	if _, ok := s.Tracks.m[name]; !ok {
		s.Infoln("Track", name, "added")
		if s.Tracks.m[name] = t; s.Tracks.Err() == nil {
			for _, ch := range s.Tracks.waiters[name] {
				if *ch != nil {
					*ch <- t
					close(*ch)
					*ch = nil //通过设置为nil，防止重复通知
				}
			}
		}
	}
}

// func (ts *Tracks) GetTrack(name string) Track {
// 	ts.RLock()
// 	defer ts.RUnlock()
// 	return ts.m[name]
// }

// WaitDone 当等待结束时需要调用该函数，防止订阅者无限等待Track
func (ts *Tracks) WaitDone() {
	ts.Lock()
	defer ts.Unlock()
	for _, chs := range ts.waiters {
		for _, ch := range chs {
			if *ch != nil {
				close(*ch)
				*ch = nil //通过设置为nil，防止重复关闭
			}
		}
	}
}
func (ts *Tracks) WaitTrack(names ...string) (ch chan Track) {
	ch = make(chan Track, 1)
	ts.Lock()
	defer ts.Unlock()
	for _, name := range names {
		if t, ok := ts.m[name]; ok {
			ch <- t
			return
		}
	}
	if ts.Err() == nil { //在等待时间范围内
		for _, name := range names {
			ts.waiters[name] = append(ts.waiters[name], &ch)
		}
	} else {
		close(ch)
	}
	return
}
