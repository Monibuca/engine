package engine

import (
	"context"
	"net/url"
	"sync"
	"time"

	"github.com/pkg/errors"
)

// Subscriber 订阅者实体定义
type Subscriber struct {
	context.Context `json:"-"`
	cancel          context.CancelFunc
	Ctx2            context.Context `json:"-"`
	*Stream         `json:"-"`
	ID              string
	TotalDrop       int //总丢帧
	TotalPacket     int
	Type            string
	BufferLength    int
	Delay           uint32
	SubscribeTime   time.Time
	SubscribeArgs   url.Values
	OnAudio         func(pack AudioPack) `json:"-"`
	OnVideo         func(pack VideoPack) `json:"-"`
	closeOnce       sync.Once
}

func (s *Subscriber) close() {
	if s.Stream != nil {
		s.UnSubscribe(s)
	}
	if s.cancel != nil {
		s.cancel()
	}
}

// Close 关闭订阅者
func (s *Subscriber) Close() {
	s.closeOnce.Do(s.close)
}

//Subscribe 开始订阅 将Subscriber与Stream关联
func (s *Subscriber) Subscribe(streamPath string) error {
	u, _ := url.Parse(streamPath)
	s.SubscribeArgs, _ = url.ParseQuery(u.RawQuery)
	streamPath = u.Path
	if stream := FindStream(streamPath); stream == nil {
		return errors.Errorf("Stream not found:%s", streamPath)
	} else {
		if stream.Subscribe(s); s.Context == nil {
			return errors.Errorf("stream not exist:%s", streamPath)
		}
	}
	return nil
}

//Play 开始播放
func (s *Subscriber) Play(at *AudioTrack, vt *VideoTrack) {
	defer s.Close()
	if vt == nil && at == nil {
		return
	}
	if vt == nil {
		s.PlayAudio(at)
		return
	} else if at == nil {
		s.PlayVideo(vt)
		return
	}
	var extraExit <-chan struct{}
	if s.Ctx2 != nil {
		extraExit = s.Ctx2.Done()
	}
	streamExit := s.Context.Done()
	select {
	case <-vt.WaitIDR.Done(): //等待获取到第一个关键帧
	case <-streamExit: //可能等不到关键帧就退出了
		return
	case <-extraExit: //可能等不到关键帧就退出了
		return
	}
	vr := vt.SubRing(vt.IDRing) //从关键帧开始读取，首屏秒开
	ar := at.Clone()
	vp := vr.Read().(*VideoPack)
	ap := ar.Read().(*AudioPack)
	vst, ast := vp.Timestamp, ap.Timestamp
	for vt.Goon() {
		select {
		case <-extraExit:
			return
		case <-streamExit:
			return
		default:
			if ap.Timestamp > vp.Timestamp || ap.Timestamp == 0 {
				s.OnVideo(vp.Copy(vst))
				vr.MoveNext()
				vp = vr.Read().(*VideoPack)
			} else {
				s.OnAudio(ap.Copy(ast))
				ar.MoveNext()
				ap = ar.Read().(*AudioPack)
			}
		}
	}
}
func (s *Subscriber) PlayAudio(at *AudioTrack) {
	at.Play(s.Ctx2, s.OnAudio)
}

func (s *Subscriber) PlayVideo(vt *VideoTrack) {
	vt.Play(s.Ctx2, s.OnVideo)
}
