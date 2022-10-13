package engine

import (
	"context"
	"errors"
	"io"
	"net/url"
	"reflect"
	"strings"
	"time"

	"go.uber.org/zap"
	"m7s.live/engine/v4/config"
	"m7s.live/engine/v4/util"
)

type IOConfig interface {
	config.Publish | config.Subscribe
}
type ClientConfig interface {
	config.Pull | config.Push
}

// 发布者或者订阅者的共用结构体
type IO[C IOConfig] struct {
	ID                 string
	Type               string
	context.Context    `json:"-"` //不要直接设置，应当通过OnEvent传入父级Context
	context.CancelFunc `json:"-"` //流关闭是关闭发布者或者订阅者
	*zap.Logger        `json:"-"`
	StartTime          time.Time //创建时间
	Stream             *Stream   `json:"-"`
	io.Reader          `json:"-"`
	io.Writer          `json:"-"`
	io.Closer          `json:"-"`
	Args               url.Values
	Config             *C  `json:"-"`
	Spesic             IIO `json:"-"`
}

func (io *IO[C]) IsClosed() bool {
	return io.Err() != nil
}

// SetIO（可选） 设置Writer、Reader、Closer
func (i *IO[C]) SetIO(conn any) {
	if v, ok := conn.(io.Closer); ok {
		i.Closer = v
	}
	if v, ok := conn.(io.Reader); ok {
		i.Reader = v
	}
	if v, ok := conn.(io.Writer); ok {
		i.Writer = v
	}
}

// SetParentCtx（可选）
func (i *IO[C]) SetParentCtx(parent context.Context) {
	i.Context, i.CancelFunc = context.WithCancel(parent)
}

func (i *IO[C]) OnEvent(event any) {
	switch event.(type) {
	case SEclose, SEKick:
		if i.Closer != nil {
			i.Closer.Close()
		}
		if i.CancelFunc != nil {
			i.CancelFunc()
		}
	}
}
func (io *IO[C]) GetStream() *Stream {
	return io.Stream
}
func (io *IO[C]) GetIO() *IO[C] {
	return io
}

func (io *IO[C]) GetConfig() *C {
	return io.Config
}

type IIO interface {
	IsClosed() bool
	OnEvent(any)
	Stop()
	SetIO(any)
	SetParentCtx(context.Context)
	GetStream() *Stream
}

//Stop 停止订阅或者发布，由订阅者或者发布者调用
func (io *IO[C]) Stop() {
	if io.CancelFunc != nil {
		io.CancelFunc()
	}
}

var BadNameErr = errors.New("Bad Name")
var StreamIsClosedErr = errors.New("Stream Is Closed")

// receive 用于接收发布或者订阅
func (io *IO[C]) receive(streamPath string, specific IIO, conf *C) error {
	streamPath = strings.Trim(streamPath, "/")
	u, err := url.Parse(streamPath)
	if err != nil {
		io.Error("receive streamPath wrong format", zap.String("streamPath", streamPath), zap.Error(err))
		return err
	}
	io.Args = u.Query()
	wt := time.Second * 5
	var c any = conf
	if v, ok := c.(*config.Subscribe); ok {
		wt = util.Second2Duration(v.WaitTimeout)
	}
	if io.Context == nil {
		io.Context, io.CancelFunc = context.WithCancel(Engine)
	}
	Streams.Lock()
	s, create := findOrCreateStream(u.Path, wt)
	Streams.Unlock()
	if s == nil {
		return BadNameErr
	}
	io.Config = conf
	if io.Type == "" {
		io.Type = reflect.TypeOf(specific).Elem().Name()
	}
	if v, ok := c.(*config.Publish); ok {
		io.Type = strings.TrimSuffix(io.Type, "Publisher")
		oldPublisher := s.Publisher
		if oldPublisher != nil && !oldPublisher.IsClosed() {
			// 根据配置是否剔出原来的发布者
			if v.KickExist {
				s.Warn("kick", zap.String("type", oldPublisher.GetIO().Type))
				oldPublisher.OnEvent(SEKick{})
			} else {
				return BadNameErr
			}
		}
		s.PublishTimeout = util.Second2Duration(v.PublishTimeout)
		s.DelayCloseTimeout = util.Second2Duration(v.DelayCloseTimeout)
		defer func() {
			if err == nil {
				if oldPublisher == nil {
					specific.OnEvent(specific)
				} else {
					specific.OnEvent(oldPublisher)
				}
			}
		}()
		if promise := util.NewPromise[IPublisher, struct{}](specific.(IPublisher)); s.Receive(promise) {
			return promise.Catch()
		}
	} else {
		io.Type = strings.TrimSuffix(io.Type, "Subscriber")
		if create {
			EventBus <- s // 通知发布者按需拉流
		}
		EventBus <- specific // 全局广播订阅事件
		defer func() {
			if err == nil {
				specific.OnEvent(specific)
			}
		}()
		if promise := util.NewPromise[ISubscriber, struct{}](specific.(ISubscriber)); s.Receive(promise) {
			return promise.Catch()
		}
	}
	return StreamIsClosedErr
}

// ClientIO 作为Client角色(Puller，Pusher)的公共结构体
type ClientIO[C ClientConfig] struct {
	Config         *C
	StreamPath     string // 本地流标识
	RemoteURL      string // 远程服务器地址（用于推拉）
	ReConnectCount int    //重连次数
}

func (c *ClientIO[C]) init(streamPath string, url string, conf *C) {
	c.Config = conf
	c.StreamPath = streamPath
	c.RemoteURL = url
}
