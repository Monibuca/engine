package engine

import (
	"encoding/json"

	"go.uber.org/zap"
	"m7s.live/engine/v4/common"
	"m7s.live/engine/v4/config"
)

type Subscribers struct {
	public      map[ISubscriber]*waitTracks
	internal    map[ISubscriber]*waitTracks
	waits       map[*waitTracks]struct{}
	waitAborted bool // 不再等待了
}

func (s *Subscribers) Init() {
	s.public = make(map[ISubscriber]*waitTracks)
	s.internal = make(map[ISubscriber]*waitTracks)
	s.waits = make(map[*waitTracks]struct{})
}

func (s *Subscribers) MarshalJSON() ([]byte, error) {
	var subers []ISubscriber
	for suber := range s.public {
		subers = append(subers, suber)
	}
	return json.Marshal(subers)
}

func (s *Subscribers) Broadcast(event any) {
	for sub := range s.internal {
		sub.OnEvent(event)
	}
	for sub := range s.public {
		sub.OnEvent(event)
	}
}

func (s *Subscribers) Pick() ISubscriber {
	for sub := range s.public {
		return sub
	}
	return nil
}

func (s *Subscribers) Len() int {
	return len(s.public)
}

func (s *Subscribers) RangeAll(f func(sub ISubscriber, wait *waitTracks)) {
	for sub, wait := range s.internal {
		f(sub, wait)
	}
	for sub, wait := range s.public {
		f(sub, wait)
	}
}

func (s *Subscribers) OnTrack(track common.Track) {
	s.RangeAll(func(sub ISubscriber, wait *waitTracks) {
		if _, ok := s.waits[wait]; ok {
			if wait.Accept(track) {
				delete(s.waits, wait)
			}
		} else {
			sub.OnEvent(track)
		}
	})
}

func (s *Subscribers) OnPublisherLost(event StateEvent) {
	s.RangeAll(func(sub ISubscriber, wait *waitTracks) {
		if _, ok := s.waits[wait]; ok {
			wait.Reject(ErrPublisherLost)
			delete(s.waits, wait)
		}
		sub.OnEvent(event)
	})
}

func (s *Subscribers) AbortWait() {
	s.waitAborted = true
	for wait := range s.waits {
		wait.Resolve()
		delete(s.waits, wait)
	}
}

func (s *Subscribers) Delete(suber ISubscriber) {
	delete(s.public, suber)
	io := suber.GetSubscriber()
	io.Info("suber -1", zap.Int("remains", s.Len()))
	if config.Global.EnableSubEvent {
		EventBus <- UnsubscribeEvent{suber}
	}
}

func (s *Subscribers) Add(suber ISubscriber, wait *waitTracks) {
	io := suber.GetSubscriber()
	if io.IsInternal {
		s.internal[suber] = wait
		io.Info("innersuber +1", zap.Int("remains", len(s.internal)))
	} else {
		s.public[suber] = wait
		io.Info("suber +1", zap.Int("remains", s.Len()))
		if config.Global.EnableSubEvent {
			EventBus <- suber
		}
	}
	if wait.NeedWait() {
		s.waits[wait] = struct{}{}
	} else {
		wait.Resolve()
	}
}

func (s *Subscribers) Dispose() {
	for w := range s.waits {
		w.Reject(ErrStreamIsClosed)
	}
	s.waits = nil
	s.public = nil
	s.internal = nil
}
