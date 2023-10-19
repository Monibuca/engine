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
	waits       map[*waitTracks]ISubscriber
	waitAborted bool // 不再等待了
}

func (s *Subscribers) Init() {
	s.public = make(map[ISubscriber]*waitTracks)
	s.internal = make(map[ISubscriber]*waitTracks)
	s.waits = make(map[*waitTracks]ISubscriber)
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

func (s *Subscribers) RangeAll(f func(sub ISubscriber)) {
	s.rangeAll(func(sub ISubscriber, wait *waitTracks) {
		f(sub)
	})
}

func (s *Subscribers) rangeAll(f func(sub ISubscriber, wait *waitTracks)) {
	for sub, wait := range s.internal {
		f(sub, wait)
	}
	for sub, wait := range s.public {
		f(sub, wait)
	}
}

func (s *Subscribers) OnTrack(track common.Track) {
	s.rangeAll(func(sub ISubscriber, wait *waitTracks) {
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
	s.rangeAll(func(sub ISubscriber, wait *waitTracks) {
		if _, ok := s.waits[wait]; ok {
			wait.Reject(ErrPublisherLost)
			delete(s.waits, wait)
		}
		sub.OnEvent(event)
	})
}

// SendInviteTrack 广播需要的 Track（转码插件可以用到）
func (s *Subscribers) SendInviteTrack(stream *Stream) {
	var video = map[string]ISubscriber{}
	var audio = map[string]ISubscriber{}
	for wait, suber := range s.waits {
		for _, name := range wait.video {
			video[name] = suber
		}
		for _, name := range wait.audio {
			audio[name] = suber
		}
	}
	for v, suber := range video {
		InviteTrack(v, suber)
	}
	for a, suber := range audio {
		InviteTrack(a, suber)
	}
}

func (s *Subscribers) AbortWait() {
	s.waitAborted = true
	for wait := range s.waits {
		wait.Resolve()
		delete(s.waits, wait)
	}
}

func (s *Subscribers) Find(id string) ISubscriber {
	for sub := range s.public {
		if sub.GetSubscriber().ID == id {
			return sub
		}
	}
	return nil
}

func (s *Subscribers) Delete(suber ISubscriber) {
	io := suber.GetSubscriber()
	for _, reader := range io.readers {
		reader.Track.Debug("reader -1", zap.Int32("count", reader.Track.ReaderCount.Add(-1)))
	}
	if _, ok := s.public[suber]; ok {
		delete(s.public, suber)
		io.Info("suber -1", zap.Int("remains", s.Len()))
	}
	if _, ok := s.internal[suber]; ok {
		delete(s.internal, suber)
		io.Info("innersuber -1", zap.Int("remains", len(s.internal)))
	}
	if config.Global.EnableSubEvent {
		EventBus <- UnsubscribeEvent{CreateEvent(suber)}
	}
}

func (s *Subscribers) Add(suber ISubscriber, wait *waitTracks) {
	io := suber.GetSubscriber()
	if io.Config.Internal {
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
		s.waits[wait] = suber
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
