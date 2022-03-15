package engine

import (
	"m7s.live/engine/v4/common"
	"m7s.live/engine/v4/config"
)

type IPublisher interface {
	IIO
	GetConfig() *config.Publish
	receive(string, IPublisher, *config.Publish) error
	getIO() *IO[config.Publish, IPublisher]
	getAudioTrack() common.AudioTrack
	getVideoTrack() common.VideoTrack
}

type Publisher struct {
	IO[config.Publish, IPublisher]
	common.AudioTrack
	common.VideoTrack
}
func (p *Publisher) Stop()  {
	p.IO.Stop()
	p.Stream.Receive(ACTION_PUBLISHLOST)
}
func (p *Publisher) getAudioTrack() common.AudioTrack {
	return p.AudioTrack
}
func (p *Publisher) getVideoTrack() common.VideoTrack {
	return p.VideoTrack
}
func (p *Publisher) OnEvent(event any) {
	switch v := event.(type) {
	case IPublisher:
		if v.getIO() == p.getIO() { //第一任
			p.AudioTrack = p.Stream.NewAudioTrack()
			p.VideoTrack = p.Stream.NewVideoTrack()
		} else { // 使用前任的track，因为订阅者都挂在前任的上面
			p.AudioTrack = v.getAudioTrack()
			p.VideoTrack = v.getVideoTrack()
		}
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
