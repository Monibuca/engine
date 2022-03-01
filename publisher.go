package engine

import (
	"m7s.live/engine/v4/common"
	"m7s.live/engine/v4/config"
)

type IPublisher interface {
	IIO
	GetConfig() *config.Publish
	receive(string, IPublisher, *config.Publish) error
}

type Publisher struct {
	IO[config.Publish, IPublisher]
	common.AudioTrack
	common.VideoTrack
}

func (p *Publisher) OnEvent(event any) {
	switch v := event.(type) {
	case *Stream:
		p.AudioTrack = v.NewAudioTrack()
		p.VideoTrack = v.NewVideoTrack()
	}
	p.IO.OnEvent(event)
}

type IPuller interface {
	IPublisher
	Connect() error
	Pull()
	Reconnect() bool
	init(streamPath string, url string, conf *config.Pull)
}

// 用于远程拉流的发布者
type Puller struct {
	Client[config.Pull]
}

// 是否需要重连
func (pub *Puller) Reconnect() (ok bool) {
	ok = pub.Config.RePull == -1 || pub.ReConnectCount <= pub.Config.RePull
	pub.ReConnectCount++
	return
}
