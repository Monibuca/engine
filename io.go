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
	"m7s.live/engine/v4/log"
	"m7s.live/engine/v4/util"
)

type IOConfig interface {
	config.Publish | config.Subscribe
}
type ClientConfig interface {
	config.Pull | config.Push
}

type AuthSub interface {
	OnAuth(*util.Promise[ISubscriber]) error
}

type AuthPub interface {
	OnAuth(*util.Promise[IPublisher]) error
}

// 发布者或者订阅者的共用结构体
type IO struct {
	ID                 string
	Type               string
	context.Context    `json:"-"` //不要直接设置，应当通过OnEvent传入父级Context
	context.CancelFunc `json:"-"` //流关闭是关闭发布者或者订阅者
	*log.Logger        `json:"-"`
	StartTime          time.Time //创建时间
	Stream             *Stream   `json:"-"`
	io.Reader          `json:"-"`
	io.Writer          `json:"-"`
	io.Closer          `json:"-"`
	Args               url.Values
	Spesific           IIO `json:"-"`
}

func (io *IO) IsClosed() bool {
	return io.Err() != nil
}

// SetIO（可选） 设置Writer、Reader、Closer
func (i *IO) SetIO(conn any) {
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
func (i *IO) SetParentCtx(parent context.Context) {
	i.Context, i.CancelFunc = context.WithCancel(parent)
}

func (i *IO) SetLogger(logger *log.Logger) {
	i.Logger = logger
}

func (i *IO) OnEvent(event any) {
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

func (io *IO) IsShutdown() bool {
	if io.Stream == nil {
		return false
	}
	return io.Stream.IsShutdown()
}

type IIO interface {
	receive(string, IIO) error
	IsClosed() bool
	OnEvent(any)
	Stop()
	SetIO(any)
	SetParentCtx(context.Context)
	SetLogger(*log.Logger)
	IsShutdown() bool
}

// Stop 停止订阅或者发布，由订阅者或者发布者调用
func (io *IO) Stop() {
	if io.IsClosed() {
		return
	}
	io.Debug("stop", zap.Stack("stack"))
	if io.CancelFunc != nil {
		io.CancelFunc()
	}
}

var (
	ErrBadStreamName  = errors.New("Stream Already Exist")
	ErrBadTrackName   = errors.New("Track Already Exist")
	ErrStreamIsClosed = errors.New("Stream Is Closed")
	ErrPublisherLost  = errors.New("Publisher Lost")
	OnAuthSub         func(p *util.Promise[ISubscriber]) error
	OnAuthPub         func(p *util.Promise[IPublisher]) error
)

// receive 用于接收发布或者订阅
func (io *IO) receive(streamPath string, specific IIO) error {
	streamPath = strings.Trim(streamPath, "/")
	u, err := url.Parse(streamPath)
	if err != nil {
		if EngineConfig.LogLang == "zh" {
			io.Error("接收流路径(流唯一标识)格式错误,必须形如 live/test ", zap.String("流路径", streamPath), zap.Error(err))
		} else {
			io.Error("receive streamPath wrong format", zap.String("streamPath", streamPath), zap.Error(err))
		}
		return err
	}
	io.Args = u.Query()
	wt := time.Second * 5
	if v, ok := specific.(ISubscriber); ok {
		wt = v.GetSubscriber().Config.WaitTimeout
	}
	if io.Context == nil {
		io.Context, io.CancelFunc = context.WithCancel(Engine)
	}
	Streams.Lock()
	s, create := findOrCreateStream(u.Path, wt)
	Streams.Unlock()
	if s == nil {
		return ErrBadStreamName
	}
	io.Stream = s
	io.Spesific = specific
	io.StartTime = time.Now()
	if io.Type == "" {
		io.Type = reflect.TypeOf(specific).Elem().Name()
	}
	io.Logger = s.With(zap.String("type", io.Type))
	if io.ID != "" {
		io.Logger = io.Logger.With(zap.String("ID", io.ID))
	}
	if v, ok := specific.(IPublisher); ok {
		conf := v.GetPublisher().Config
		io.Type = strings.TrimSuffix(io.Type, "Publisher")
		io.Info("publish")
		oldPublisher := s.Publisher
		if oldPublisher != nil && !oldPublisher.IsClosed() {
			// 根据配置是否剔出原来的发布者
			if conf.KickExist {
				s.Warn("kick", zap.String("type", oldPublisher.GetPublisher().Type))
				oldPublisher.OnEvent(SEKick{})
			} else if oldPublisher == specific {
				//断线重连
			} else {
				return ErrBadStreamName
			}
		}
		s.PublishTimeout = conf.PublishTimeout
		s.DelayCloseTimeout = conf.DelayCloseTimeout
		defer func() {
			if err == nil {
				if oldPublisher == nil {
					specific.OnEvent(specific)
				} else {
					specific.OnEvent(oldPublisher)
				}
			}
		}()
		if config.Global.EnableAuth {
			onAuthPub := OnAuthPub
			if auth, ok := specific.(AuthPub); ok {
				onAuthPub = auth.OnAuth
			}
			if onAuthPub != nil {
				authPromise := util.NewPromise(specific.(IPublisher))
				if err = onAuthPub(authPromise); err == nil {
					err = authPromise.Await()
				}
				if err != nil {
					return err
				}
			}
		}
		if promise := util.NewPromise(specific.(IPublisher)); s.Receive(promise) {
			err = promise.Await()
			return err
		}
	} else {
		io.Type = strings.TrimSuffix(io.Type, "Subscriber")
		io.Info("subscribe")
		if create {
			EventBus <- s // 通知发布者按需拉流
		}
		defer func() {
			if err == nil {
				specific.OnEvent(specific)
			}
		}()
		if config.Global.EnableAuth {
			onAuthSub := OnAuthSub
			if auth, ok := specific.(AuthSub); ok {
				onAuthSub = auth.OnAuth
			}
			if onAuthSub != nil {
				authPromise := util.NewPromise(specific.(ISubscriber))
				if err = onAuthSub(authPromise); err == nil {
					err = authPromise.Await()
				}
				if err != nil {
					return err
				}
			}
		}
		if promise := util.NewPromise(specific.(ISubscriber)); s.Receive(promise) {
			err = promise.Await()
			return err
		}
	}
	return ErrStreamIsClosed
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
