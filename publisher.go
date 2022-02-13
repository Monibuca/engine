package engine

import (
	"io"
	"reflect"
	"time"

	"github.com/Monibuca/engine/v4/config"
	log "github.com/sirupsen/logrus"
)

type IPublisher interface {
	Close() // 流关闭时或者被踢时触发
	OnStateChange(oldState StreamState, newState StreamState) bool
	OnStateChanged(oldState StreamState, newState StreamState)
	Publish(streamPath string, specific IPublisher, config config.Publish) bool
}

type IPuller interface {
	IPublisher
	Pull(int)
}
type Publisher struct {
	Type     string
	Config   *config.Publish
	*Stream  `json:"-"`
	specific IPublisher
	*log.Entry
}

func (pub *Publisher) Publish(streamPath string, specific IPublisher, config config.Publish) bool {
	Streams.Lock()
	defer Streams.Unlock()
	s, created := findOrCreateStream(streamPath, time.Second)
	if s.IsClosed() {
		return false
	}
	if s.Publisher != nil {
		if config.KickExsit {
			s.Warn("kick", s.Publisher)
			s.Publisher.Close()
		} else {
			s.Warn("publisher exsit", s.Publisher)
			return false
		}
	}
	pub.Stream = s
	pub.specific = specific
	pub.Config = &config
	s.Publisher = specific
	if pub.Type == "" {
		pub.Type = reflect.TypeOf(specific).Elem().Name()
	}
	pub.Entry = s.WithField("puber", pub.Type)
	if created {
		s.PublishTimeout = config.PublishTimeout.Duration()
		s.WaitCloseTimeout = config.WaitCloseTimeout.Duration()
	}
	s.actionChan <- PublishAction{}
	return true
}

func (pub *Publisher) OnStateChange(oldState StreamState, newState StreamState) bool {
	return true
}
func (pub *Publisher) OnStateChanged(oldState StreamState, newState StreamState) {
}

// 用于远程拉流的发布者
type Puller struct {
	Publisher
	Config    *config.Pull
	RemoteURL string
	io.Reader
	io.Closer
	pullCount int
}

// 是否需要重连
func (pub *Puller) reconnect() bool {
	return pub.Config.RePull == -1 || pub.pullCount <= pub.Config.RePull
}

func (pub *Puller) pull() {
	pub.specific.(IPuller).Pull(pub.pullCount)
	pub.pullCount++
}

func (pub *Puller) OnStateChanged(oldState StreamState, newState StreamState) {
	switch newState {
	case STATE_WAITTRACK:
		go pub.pull()
	case STATE_WAITPUBLISH:
		if pub.reconnect() && pub.Publish(pub.Path, pub.specific, *pub.Publisher.Config) {
			go pub.pull()
		}
	}
}

func (p *Puller) Close() {
	if p.Closer != nil {
		p.Closer.Close()
	}
}
