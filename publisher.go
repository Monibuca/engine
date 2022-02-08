package engine

import (
	"io"
	"net/url"
	"time"

	"github.com/Monibuca/engine/v4/config"
)

type IPublisher interface {
	Close() // 流关闭时或者被踢时触发
	OnStateChange(oldState StreamState, newState StreamState) bool
}

type Publisher struct {
	Type    string
	*Stream `json:"-"`
}

func (pub *Publisher) Publish(streamPath string, realPub IPublisher, config config.Publish) bool {
	Streams.Lock()
	defer Streams.Unlock()
	s, created := findOrCreateStream(streamPath, time.Second)
	if s.IsClosed() {
		return false
	}
	if s.Publisher != nil {
		if config.KillExit {
			s.Publisher.Close()
		} else {
			return false
		}
	}
	pub.Stream = s
	s.Publisher = realPub
	if created {
		s.PublishTimeout = config.PublishTimeout.Duration()
		s.WaitCloseTimeout = config.WaitCloseTimeout.Duration()
		go s.run()
	}
	s.actionChan <- PublishAction{}
	return true
}

func (pub *Publisher) OnStateChange(oldState StreamState, newState StreamState) bool {
	return true
}

// 用于远程拉流的发布者
type Puller struct {
	Publisher
	RemoteURL *url.URL
	io.ReadCloser
}

func (puller *Puller) Close() {
	puller.ReadCloser.Close()
}
