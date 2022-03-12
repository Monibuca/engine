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

// SetIO（可选） 设置Writer、Reader、Closer
func (i *IO[C, S]) SetIO(conn any) {
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
func (i *IO[C, S]) SetParentCtx(parent context.Context) {
	i.Context, i.CancelFunc = context.WithCancel(parent)
}

func (i *IO[C, S]) OnEvent(event any) {
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

func (io *IO[C, S]) getIO() *IO[C, S] {
	return io
}

func (io *IO[C, S]) GetConfig() *C {
	return io.Config
}

type IIO interface {
	IsClosed() bool
	OnEvent(any)
	Stop()
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
	s, create := findOrCreateStream(u.Path, wt)
	if s == nil {
		return BadNameErr
	}
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
	} else if create {
		EventBus <- s //通知发布者按需拉流
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

func (c *Client[C]) init(streamPath string, url string, conf *C) {
	c.Config = conf
	c.StreamPath = streamPath
	c.RemoteURL = url
}
