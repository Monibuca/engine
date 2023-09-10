package engine

import (
	"context"
	"io"
	"strings"
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
	streamPath := pub.StreamPath
	if i := strings.Index(streamPath, "?"); i >= 0 {
		streamPath = streamPath[:i]
	}
	if _, loaded := Pullers.LoadOrStore(streamPath, puller); loaded {
		puller.Error("puller already exists")
		return
	}
	defer func() {
		Pullers.Delete(streamPath)
		puller.Disconnect()
		if stream != nil {
			stream.Close()
		}
	}()
	puber := puller.GetPublisher()
	startTime := time.Now()
	for puller.Info("start pull"); puller.Reconnect(); puller.Warn("restart pull") {
		if time.Since(startTime) < 5*time.Second {
			time.Sleep(5 * time.Second)
		}
		startTime = time.Now()
		if err = puller.Connect(); err != nil {
			if err == io.EOF {
				puller.Info("pull complete")
				return
			}
			puller.Error("pull connect", zap.Error(err))
			if badPuller {
				return
			}
		} else {
			if err = puller.Publish(pub.StreamPath, puller); err != nil {
				puller.Error("pull publish", zap.Error(err))
				return
			}
			s := puber.Stream
			if stream != s && stream != nil { // 这段代码说明老流已经中断，创建了新流，需要把track置空，从而避免复用
				puber.AudioTrack = nil
				puber.VideoTrack = nil
				puber.Context, puber.CancelFunc = context.WithCancel(Engine) // 老流的上下文已经取消，需要重新创建
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
