package engine

import (
	"context"
	"io"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/Monibuca/engine/v4/config"
	"go.uber.org/zap"
)

type IOConfig interface {
	config.Publish | config.Subscribe
}

type IO[C IOConfig] struct {
	ID   string
	Type string
	context.Context
	context.CancelFunc
	*zap.Logger
	StartTime time.Time //创建时间
	Stream    *Stream   `json:"-"`
	io.Reader `json:"-"`
	io.Writer `json:"-"`
	io.Closer `json:"-"`
	Args      url.Values
	Config    *C
}

func (io *IO[C]) IsClosed() bool {
	return io.Err() != nil
}
func (io *IO[C]) OnEvent(event any) any {
	switch v := event.(type) {
	case context.Context:
		io.Context, io.CancelFunc = context.WithCancel(v)
	case *Stream:
		io.StartTime = time.Now()
		io.Stream = v
		io.Logger = v.With(zap.String("type", io.Type))
		if io.ID != "" {
			io.Logger = io.Logger.With(zap.String("ID", io.ID))
		}
	case SEclose, SEKick:
		if io.Closer != nil {
			io.Closer.Close()
		}
		if io.CancelFunc != nil {
			io.CancelFunc()
		}
	}
	return event
}
func (io *IO[C]) getID() string {
	return io.ID
}
func (io *IO[C]) getType() string {
	return io.Type
}

type IIO interface {
	IsClosed() bool
	OnEvent(any) any
	getID() string
	getType() string
}

func (io *IO[C]) bye(specific any) {
	if io.CancelFunc != nil {
		io.CancelFunc()
	}
	if io.Stream != nil {
		io.Stream.Receive(specific)
	}
}

func (io *IO[C]) receive(streamPath string, specific any, conf *C) bool {
	Streams.Lock()
	defer Streams.Unlock()
	streamPath = strings.Trim(streamPath, "/")
	u, err := url.Parse(streamPath)
	if err != nil {
		return false
	}
	io.Args = u.Query()
	wt := time.Second
	var c any = conf
	if v, ok := c.(*config.Subscribe); ok {
		wt = v.WaitTimeout.Duration()
	}
	if io.Context == nil {
		io.Context, io.CancelFunc = context.WithCancel(Engine)
	}
	s, created := findOrCreateStream(u.Path, wt)
	if s.IsClosed() {
		return false
	}
	if v, ok := c.(*config.Publish); ok {
		if s.Publisher != nil && !s.Publisher.IsClosed() {
			// 根据配置是否剔出原来的发布者
			if v.KickExist {
				s.Warn("kick", zap.Any("publisher", s.Publisher))
				s.Publisher.OnEvent(SEKick{})
			} else {
				s.Warn("publisher exist", zap.Any("publisher", s.Publisher))
				return false
			}
		}
		if created {
			s.PublishTimeout = v.PublishTimeout.Duration()
			s.WaitCloseTimeout = v.WaitCloseTimeout.Duration()
		}
	} else {
		Bus.Publish(Event_REQUEST_PUBLISH, s)
	}
	if io.Type == "" {
		io.Type = reflect.TypeOf(specific).Elem().Name()
	}
	if s.Receive(specific); io.Stream != nil {
		io.Config = conf
		return true
	}
	return false
}
