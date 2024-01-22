package engine

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
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
	ID                      string
	Type                    string
	RemoteAddr              string
	context.Context         `json:"-" yaml:"-"` //不要直接设置，应当通过SetParentCtx传入父级Context
	context.CancelCauseFunc `json:"-" yaml:"-"` //流关闭是关闭发布者或者订阅者
	*log.Logger             `json:"-" yaml:"-"`
	StartTime               time.Time //创建时间
	Stream                  *Stream   `json:"-" yaml:"-"`
	io.Reader               `json:"-" yaml:"-"`
	io.Writer               `json:"-" yaml:"-"`
	io.Closer               `json:"-" yaml:"-"`
	Args                    url.Values
	Spesific                IIO `json:"-" yaml:"-"`
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
	i.Context, i.CancelCauseFunc = context.WithCancelCause(parent)
}

func (i *IO) SetLogger(logger *log.Logger) {
	i.Logger = logger
}

func (i *IO) OnEvent(event any) {
	switch event.(type) {
	case SEclose:
		i.close(StopError{zap.String("event", "close")})
	case SEKick:
		i.close(StopError{zap.String("event", "kick")})
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
	Stop(reason ...zapcore.Field)
	SetIO(any)
	SetParentCtx(context.Context)
	SetLogger(*log.Logger)
	IsShutdown() bool
	log.Zap
}

func (i *IO) close(err StopError) bool {
	if i.IsClosed() {
		i.Warn("already closed", err...)
		return false
	}
	i.Info("close", err...)
	if i.CancelCauseFunc != nil {
		i.CancelCauseFunc(err)
	}
	if i.Closer != nil {
		i.Closer.Close()
	}
	return true
}

// Stop 停止订阅或者发布，由订阅者或者发布者调用
func (io *IO) Stop(reason ...zapcore.Field) {
	io.close(StopError(reason))
}

type StopError []zapcore.Field

func (s StopError) Error() string {
	return "stop"
}

var (
	ErrDuplicatePublish = errors.New("Duplicate Publish")
	ErrBadStreamName    = errors.New("StreamPath Illegal")
	ErrBadTrackName     = errors.New("Track Already Exist")
	ErrTrackMute        = errors.New("Track Mute")
	ErrStreamIsClosed   = errors.New("Stream Is Closed")
	ErrPublisherLost    = errors.New("Publisher Lost")
	ErrAuth             = errors.New("Auth Failed")
	OnAuthSub           func(p *util.Promise[ISubscriber]) error
	OnAuthPub           func(p *util.Promise[IPublisher]) error
)

func (io *IO) auth(key string, secret string, expire string) bool {
	if unixTime, err := strconv.ParseInt(expire, 16, 64); err != nil || time.Now().Unix() > unixTime {
		return false
	}
	trueSecret := md5.Sum([]byte(key + io.Stream.Path + expire))
	for i := 0; i < 16; i++ {
		hex, err := strconv.ParseInt(secret[i<<1:(i<<1)+2], 16, 16)
		if trueSecret[i] != byte(hex) || err != nil {
			return false
		}
	}
	return true
}

// receive 用于接收发布或者订阅
func (io *IO) receive(streamPath string, specific IIO) error {
	streamPath = strings.Trim(streamPath, "/")
	u, err := url.Parse(streamPath)
	if err != nil {
		if EngineConfig.LogLang == "zh" {
			log.Error("接收流路径(流唯一标识)格式错误,必须形如 live/test ", zap.String("流路径", streamPath), zap.Error(err))
		} else {
			log.Error("receive streamPath wrong format", zap.String("streamPath", streamPath), zap.Error(err))
		}
		return err
	}
	io.Args = u.Query()
	wt := time.Second * 5
	var iSub ISubscriber
	var iPub IPublisher
	var isSubscribe bool
	if iSub, isSubscribe = specific.(ISubscriber); isSubscribe {
		wt = iSub.GetSubscriber().Config.WaitTimeout
	} else {
		iPub = specific.(IPublisher)
	}
	s, create := findOrCreateStream(u.Path, wt)
	if s == nil {
		return ErrBadStreamName
	}
	if io.Stream == nil { //初次
		if io.Type == "" {
			io.Type = reflect.TypeOf(specific).Elem().Name()
		}
		logFeilds := []zapcore.Field{zap.String("type", io.Type)}
		if io.ID != "" {
			logFeilds = append(logFeilds, zap.String("ID", io.ID))
		}
		if io.Logger == nil {
			io.Logger = s.With(logFeilds...)
		} else {
			io.Logger = io.Logger.With(logFeilds...)
		}
	}
	io.Stream = s
	io.Spesific = specific
	io.StartTime = time.Now()
	if io.Context == nil {
		io.Debug("create context")
		io.SetParentCtx(Engine.Context)
	} else if io.IsClosed() {
		io.Debug("recreate context")
		io.SetParentCtx(Engine.Context)
	} else {
		io.Debug("warp context")
		io.SetParentCtx(io.Context)
	}
	defer func() {
		if err == nil {
			specific.OnEvent(specific)
		}
	}()
	if !isSubscribe {
		puber := iPub.GetPublisher()
		conf := puber.Config
		io.Info("publish", zap.String("ptr", fmt.Sprintf("%p", iPub)))
		s.pubLocker.Lock()
		defer s.pubLocker.Unlock()
		if config.Global.EnableAuth {
			onAuthPub := OnAuthPub
			if auth, ok := specific.(AuthPub); ok {
				onAuthPub = auth.OnAuth
			}
			if onAuthPub != nil {
				authPromise := util.NewPromise(iPub)
				if err = onAuthPub(authPromise); err == nil {
					err = authPromise.Await()
				}
				if err != nil {
					return err
				}
			} else if conf.Key != "" {
				if !io.auth(conf.Key, io.Args.Get(conf.SecretArgName), io.Args.Get(conf.ExpireArgName)) {
					return ErrAuth
				}
			}
		}
		if promise := util.NewPromise(iPub); s.Receive(promise) {
			err = promise.Await()
			return err
		}
	} else {
		conf := iSub.GetSubscriber().Config
		io.Info("subscribe")
		if create {
			EventBus <- InvitePublish{CreateEvent(s.Path)} // 通知发布者按需拉流
		}
		if config.Global.EnableAuth && !conf.Internal {
			onAuthSub := OnAuthSub
			if auth, ok := specific.(AuthSub); ok {
				onAuthSub = auth.OnAuth
			}
			if onAuthSub != nil {
				authPromise := util.NewPromise(iSub)
				if err = onAuthSub(authPromise); err == nil {
					err = authPromise.Await()
				}
				if err != nil {
					return err
				}
			} else if conf.Key != "" {
				if !io.auth(conf.Key, io.Args.Get(conf.SecretArgName), io.Args.Get(conf.ExpireArgName)) {
					return ErrAuth
				}
			}
		}
		if promise := util.NewPromise(iSub); s.Receive(promise) {
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
