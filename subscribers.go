package engine

import (
	"encoding/json"

	"go.uber.org/zap"
	"m7s.live/engine/v4/config"
)

type Subscribers map[ISubscriber]*waitTracks

func (s Subscribers) MarshalJSON() ([]byte, error) {
	var subers []ISubscriber
	for suber := range s {
		subers = append(subers, suber)
	}
	return json.Marshal(subers)
}

func (s Subscribers) Broadcast(event any) {
	for sub := range s {
		sub.OnEvent(event)
	}
}

func (s Subscribers) Pick() ISubscriber {
	for sub := range s {
		return sub
	}
	return nil
}

func (s Subscribers) Delete(suber ISubscriber) {
	delete(s, suber)
	io := suber.GetSubscriber()
	io.Info("suber -1", zap.Int("remains", len(s)))
	if config.Global.EnableSubEvent {
		EventBus <- UnsubscribeEvent{suber}
	}
}

func (s Subscribers) Add(suber ISubscriber, wait *waitTracks) {
	s[suber] = wait
	io := suber.GetSubscriber()
	io.Info("suber +1", zap.Int("remains", len(s)))
	if config.Global.EnableSubEvent {
		EventBus <- suber
	}
}
