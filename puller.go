package engine

import (
	"io"
	"time"

	"go.uber.org/zap"
	"m7s.live/engine/v4/config"
)

var zshutdown = zap.String("reason", "shutdown")
var znomorereconnect = zap.String("reason", "no more reconnect")

type IPuller interface {
	IPublisher
	Connect() error
	Disconnect()
	Pull() error
	Reconnect() bool
	init(streamPath string, url string, conf *config.Pull)
	startPull(IPuller)
}

// 用于远程拉流的发布者
type Puller struct {
	ClientIO[config.Pull]
}

// 是否需要重连
func (pub *Puller) Reconnect() (ok bool) {
	ok = pub.Config.RePull == -1 || pub.ReConnectCount <= pub.Config.RePull
	pub.ReConnectCount++
	return
}

func (pub *Puller) startPull(puller IPuller) {
	badPuller := true
	var stream *Stream
	var err error
	Pullers.Store(puller, pub.RemoteURL)
	defer func() {
		Pullers.Delete(puller)
		puller.Disconnect()
		if stream != nil {
			stream.Close()
		}
	}()
	puber := puller.GetPublisher()
	originContext := puber.Context // 保存原始的Context
	for puller.Info("start pull"); puller.Reconnect(); puller.Warn("restart pull") {
		if err = puller.Connect(); err != nil {
			if err == io.EOF {
				puller.Info("pull complete")
				return
			}
			puller.Error("pull connect", zap.Error(err))
			if badPuller {
				return
			}
			time.Sleep(time.Second * 5)
		} else {
			puber.Context = originContext // 每次重连都需要恢复原始的Context
			if err = puller.Publish(pub.StreamPath, puller); err != nil {
				puller.Error("pull publish", zap.Error(err))
				return
			}
			s := puber.Stream
			if stream != s && stream != nil { // 这段代码说明老流已经中断，创建了新流，需要把track置空，从而避免复用
				puber.AudioTrack = nil
				puber.VideoTrack = nil
			}
			stream = s
			badPuller = false
			if err = puller.Pull(); err != nil && !puller.IsShutdown() {
				puller.Error("pull interrupt", zap.Error(err))
			}
		}
		if puller.IsShutdown() {
			puller.Info("stop pull", zshutdown)
			return
		}
		puller.Disconnect()
	}
	puller.Warn("stop pull", znomorereconnect)
}
