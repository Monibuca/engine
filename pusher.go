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
	key := pub.RemoteURL

	if oldPusher, loaded := Pushers.LoadOrStore(key, pusher); loaded {
		sub := oldPusher.(IPusher).GetSubscriber()
		pusher.Error("pusher already exists", zap.Time("createAt", sub.StartTime))
		return
	}

	defer Pushers.Delete(key)
	defer pusher.Disconnect()
	var startTime time.Time
	for pusher.Info("start push"); pusher.Reconnect(); pusher.Warn("restart push") {
		if time.Since(startTime) < 5*time.Second {
			time.Sleep(5 * time.Second)
		}
		startTime = time.Now()
		if err = pusher.Subscribe(pub.StreamPath, pusher); err != nil {
			pusher.Error("push subscribe", zap.Error(err))
		} else {
			stream := pusher.GetSubscriber().Stream
			if err = pusher.Connect(); err != nil {
				if err == io.EOF {
					pusher.Info("push complete")
					return
				}
				pusher.Error("push connect", zap.Error(err))
				time.Sleep(time.Second * 5)
				stream.Receive(Unsubscribe(pusher)) // 通知stream移除订阅者
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
