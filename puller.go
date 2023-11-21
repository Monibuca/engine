package engine

import (
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
	OnConnected()
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

func (pub *Puller) OnConnected() {
	pub.ReConnectCount = 0 // 重置重连次数
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
	if oldPuller, loaded := Pullers.LoadOrStore(streamPath, puller); loaded {
		pub := oldPuller.(IPuller).GetPublisher()
		stream = pub.Stream
		if stream != nil {
			puller.Error("puller already exists", zap.Int8("streamState", int8(stream.State)))
			if stream.State == STATE_CLOSED {
				oldPuller.(IPuller).Stop(zap.String("reason", "dead puller"))
			}
		} else {
			puller.Error("puller already exists", zap.Time("createAt", pub.StartTime))
		}
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
	var startTime time.Time
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
			if stream != puber.Stream {
				// 老流中的音视频轨道不可再使用
				puber.AudioTrack = nil
				puber.VideoTrack = nil
			}
			stream = puber.Stream
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
