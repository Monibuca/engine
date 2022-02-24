package engine

import (
	"context"
	"errors"
	"io"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/Monibuca/engine/v4/config"
	"github.com/Monibuca/engine/v4/util"
	"go.uber.org/zap"
)

type IOConfig interface {
	config.Publish | config.Subscribe
}
type ClientConfig interface {
	config.Pull | config.Push
}

type IO[C IOConfig, S IIO] struct {
	ID                 string
	Type               string
	context.Context    //不要直接设置，应当通过OnEvent传入父级Context
	context.CancelFunc //流关闭是关闭发布者或者订阅者
	*zap.Logger
	StartTime time.Time //创建时间
	Stream    *Stream   `json:"-"`
	io.Reader `json:"-"`
	io.Writer `json:"-"`
	io.Closer `json:"-"`
	Args      url.Values
	Config    *C
}

func (io *IO[C, S]) IsClosed() bool {
	return io.Err() != nil
}

func (i *IO[C, S]) OnEvent(event any) {
	switch v := event.(type) {
	case context.Context:
		//传入父级Context，如果不传入将使用Engine的Context
		i.Context, i.CancelFunc = context.WithCancel(v)
	case *Stream:
		i.Stream = v
		i.StartTime = time.Now()
		i.Logger = v.With(zap.String("type", i.Type))
		if i.ID != "" {
			i.Logger = i.Logger.With(zap.String("ID", i.ID))
		}
	case SEclose, SEKick:
		if i.Closer != nil {
			i.Closer.Close()
		}
		if i.CancelFunc != nil {
			i.CancelFunc()
		}
	default:
		if v, ok := event.(io.Closer); ok {
			i.Closer = v
		}
		if v, ok := event.(io.Reader); ok {
			i.Reader = v
		}
		if v, ok := event.(io.Writer); ok {
			i.Writer = v
		}
	}
}
func (io *IO[C, S]) getID() string {
	return io.ID
}
func (io *IO[C, S]) getType() string {
	return io.Type
}

func (io *IO[C, S]) GetConfig() *C {
	return io.Config
}

type IIO interface {
	IsClosed() bool
	OnEvent(any)
	Stop()
	getID() string
	getType() string
}

//Stop 停止订阅或者发布，由订阅者或者发布者调用
func (io *IO[C, S]) Stop() {
	if io.CancelFunc != nil {
		io.CancelFunc()
	}
}

var BadNameErr = errors.New("Bad Name")
var StreamIsClosedErr = errors.New("Stream Is Closed")

// receive 用于接收发布或者订阅
func (io *IO[C, S]) receive(streamPath string, specific S, conf *C) error {
	Streams.Lock()
	defer Streams.Unlock()
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
		wt = v.WaitTimeout.Duration()
	}
	if io.Context == nil {
		io.Context, io.CancelFunc = context.WithCancel(Engine)
	}
	s, _ := findOrCreateStream(u.Path, wt)
	io.Config = conf
	if io.Type == "" {
		io.Type = reflect.TypeOf(specific).Elem().Name()
	}
	if v, ok := c.(*config.Publish); ok {
		if s.Publisher != nil && !s.Publisher.IsClosed() {
			// 根据配置是否剔出原来的发布者
			if v.KickExist {
				s.Warn("kick", zap.Any("publisher", s.Publisher))
				s.Publisher.OnEvent(SEKick{})
			} else {
				return BadNameErr
			}
		}
		s.PublishTimeout = v.PublishTimeout.Duration()
		s.WaitCloseTimeout = v.WaitCloseTimeout.Duration()
	}
	if promise := util.NewPromise[S, struct{}](specific); s.Receive(promise) {
		return promise.Catch()
	}
	return StreamIsClosedErr
}

type Client[C ClientConfig] struct {
	Config         *C
	StreamPath     string // 本地流标识
	RemoteURL      string // 远程服务器地址（用于推拉）
	ReConnectCount int    //重连次数
}
