package engine

import (
	"github.com/Monibuca/engine/v4/common"
	"github.com/Monibuca/engine/v4/config"
)

type IPublisher interface {
	IIO
	GetPublisher() *Publisher
	receive(string, any, *config.Publish) bool
	Unpublish()
}

type Publisher struct {
	IO[config.Publish]
	common.AudioTrack
	common.VideoTrack
}

func (p *Publisher) GetPublisher() *Publisher {
	return p
}

func (p *Publisher) Unpublish() {
	p.bye(p)
}

type PullEvent int

// 用于远程拉流的发布者
type Puller struct {
	Client[config.Pull]
}

// 是否需要重连
func (pub *Puller) Reconnect() bool {
	return pub.Config.RePull == -1 || pub.ReConnectCount <= pub.Config.RePull
}
