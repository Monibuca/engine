package engine

import (
	"net/url"
	"time"
)

type IPublisher interface {
	Close() // 流关闭时或者被踢时触发
	OnStateChange(oldState StreamState, newState StreamState) bool
}

type Publisher struct {
	Type    string
	PullURL *url.URL
	*Stream `json:"-"`
	Config  PublishConfig
}

func (pub *Publisher) Publish(streamPath string, realPub IPublisher) bool {
	Streams.Lock()
	defer Streams.Unlock()
	s, created := findOrCreateStream(streamPath, time.Second)
	if s.IsClosed() {
		return false
	}
	if s.Publisher != nil && pub.Config.KillExit {
		s.Publisher.Close()
	}
	pub.Stream = s
	s.Publisher = realPub
	if created {
		s.PublishTimeout = pub.Config.PublishTimeout.Duration()
		s.WaitCloseTimeout = pub.Config.WaitCloseTimeout.Duration()
		go s.run()
	}
	s.actionChan <- PublishAction{}
	return true
}

func (pub *Publisher) OnStateChange(oldState StreamState, newState StreamState) bool {
	return true
}
