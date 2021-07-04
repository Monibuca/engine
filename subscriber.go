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
	context.Context  `json:"-"`
	cancel           context.CancelFunc
	Ctx2             context.Context `json:"-"`
	*Stream          `json:"-"`
	ID               string
	TotalDrop        int //总丢帧
	TotalPacket      int
	Type             string
	BufferLength     int
	Delay            uint32
	SubscribeTime    time.Time
	SubscribeArgs    url.Values
	OnAudio          func(pack AudioPack) `json:"-"`
	OnVideo          func(pack VideoPack) `json:"-"`
	ByteStreamFormat bool
	closeOnce        sync.Once
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
	// dropping := false //是否处于丢帧中
	vp := vr.Read().(*VideoPack)
	ap := ar.Read().(*AudioPack)
	startTimestamp := vp.Timestamp
	for vt.Flag != 2 {
		select {
		case <-extraExit:
			return
		case <-streamExit:
			return
		default:
			if ap.Timestamp > vp.Timestamp || ap.Timestamp == 0 {
				s.OnVideo(vp.Copy(startTimestamp))
				// if !dropping {
				// 	s.OnVideo(vp.Copy(startTimestamp))
				// 	if vt.lastTs - vp.Timestamp > 1000 {
				// 		dropping = true
				// 	}
				// } else if vp.IDR {
				// 	dropping = false
				// }
				vr.MoveNext()
				vp = vr.Read().(*VideoPack)
			} else {
				s.OnAudio(ap.Copy(startTimestamp))
				// if !dropping {
				// 	s.OnAudio(ap.Copy(startTimestamp))
				// 	if at.CurrentValue().(AVPack).Since(ap.Timestamp) > 1000 {
				// 		dropping = true
				// 	}
				// }
				ar.MoveNext()
				ap = ar.Read().(*AudioPack)
			}
		}
	}
}
func (s *Subscriber) PlayAudio(at *AudioTrack) {
	streamExit := s.Context.Done()
	ar := at.Clone()
	ap := ar.Read().(*AudioPack)
	startTimestamp := ap.Timestamp
	droped := 0
	var action, send func()
	drop := func() {
		if at.current().Sequence-ap.Sequence < 4 {
			action = send
		} else {
			droped++
		}
	}
	send = func() {
		if s.OnAudio(ap.Copy(startTimestamp)); at.lastTs -ap.Timestamp > 1000 {
			action = drop
		}
	}
	var extraExit <-chan struct{}
	if s.Ctx2 != nil {
		extraExit = s.Ctx2.Done()
	}
	for action = send; at.Flag != 2; ap = ar.Read().(*AudioPack) {
		select {
		case <-extraExit:
			return
		case <-streamExit:
			return
		default:
			action()
			ar.MoveNext()
		}
	}
}

func (s *Subscriber) PlayVideo(vt *VideoTrack) {
	var extraExit <-chan struct{}
	if s.Ctx2 != nil {
		extraExit = s.Ctx2.Done()
	}
	streamExit := s.Context.Done()
	select {
	case <-vt.WaitIDR.Done():
	case <-streamExit:
		return
	case <-extraExit: //可能等不到关键帧就退出了
		return
	}
	vr := vt.SubRing(vt.IDRing) //从关键帧开始读取，首屏秒开
	vp := vr.Read().(*VideoPack)
	startTimestamp := vp.Timestamp
	var action, send func()
	drop := func() {
		if vp.IDR {
			action = send
		}
	}
	send = func() {
		if s.OnVideo(vp.Copy(startTimestamp)); vt.lastTs - vp.Timestamp > 1000 {
			action = drop
		}
	}
	for action = send; vt.Flag != 2; vp = vr.Read().(*VideoPack) {
		select {
		case <-extraExit:
			return
		case <-streamExit:
			return
		default:
			action()
			vr.MoveNext()
		}
	}
}
