package engine

import (
	"github.com/Monibuca/engine/v4/common"
	"github.com/Monibuca/engine/v4/config"
)

type IPublisher interface {
	IIO
	receive(string, IPublisher, *config.Publish) bool
}

type Publisher struct {
	IO[config.Publish, IPublisher]
	common.AudioTrack
	common.VideoTrack
}

func (p *Publisher) Unpublish() {
	p.bye(p)
}

type PullEvent int

// 用于远程拉流的发布者
type Puller struct {
	Publisher
	Config    *config.Pull
	RemoteURL string
	PullCount int
}

// 是否需要重连
func (pub *Puller) Reconnect() bool {
	return pub.Config.RePull == -1 || pub.PullCount <= pub.Config.RePull
}
