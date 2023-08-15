package engine

import (
	"io"
	"time"

	"go.uber.org/zap"
	"m7s.live/engine/v4/config"
)

type IPusher interface {
	ISubscriber
	Push() error
	Connect() error
	Disconnect()
	init(string, string, *config.Push)
	Reconnect() bool
	startPush(IPusher)
}

type Pusher struct {
	ClientIO[config.Push]
}

// 是否需要重连
func (pub *Pusher) Reconnect() (result bool) {
	result = pub.Config.RePush == -1 || pub.ReConnectCount <= pub.Config.RePush
	pub.ReConnectCount++
	return
}

func (pub *Pusher) startPush(pusher IPusher) {
	badPusher := true
	var err error
	Pushers.Store(pub.RemoteURL, pusher)
	defer Pushers.Delete(pub.RemoteURL)
	defer pusher.Disconnect()
	for pusher.Info("start push"); pusher.Reconnect(); pusher.Warn("restart push") {
		if err = pusher.Subscribe(pub.StreamPath, pusher); err != nil {
			pusher.Error("push subscribe", zap.Error(err))
			time.Sleep(time.Second * 5)
		} else {
			stream := pusher.GetSubscriber().Stream
			if err = pusher.Connect(); err != nil {
				if err == io.EOF {
					pusher.Info("push complete")
					return
				}
				pusher.Error("push connect", zap.Error(err))
				time.Sleep(time.Second * 5)
				stream.Receive(pusher) // 通知stream移除订阅者
				if badPusher {
					return
				}
			} else if err = pusher.Push(); err != nil && !stream.IsClosed() {
				pusher.Error("push", zap.Error(err))
				pusher.Stop()
			}
			badPusher = false
			if stream.IsClosed() {
				pusher.Info("stop push closed")
				return
			}
		}
		pusher.Disconnect()
	}
	pusher.Warn("stop push stop reconnect")
}
