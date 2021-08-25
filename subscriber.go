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
	OnAudio         func(uint32, *AudioPack) `json:"-"`
	OnVideo         func(uint32, *VideoPack) `json:"-"`
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
	case <-vt.WaitIDR: //等待获取到第一个关键帧
	case <-streamExit: //可能等不到关键帧就退出了
		return
	case <-extraExit: //可能等不到关键帧就退出了
		return
	}
	vr := vt.SubRing(vt.IDRing)      //从关键帧开始读取，首屏秒开
	realSt := vt.PreItem().Timestamp // 当前时间戳
	ar := at.Clone()
	iv, vp := vr.Read()
	ia, ap := ar.Read()
	vst := iv.Timestamp
	chase := true
	for {
		select {
		case <-extraExit:
			return
		case <-streamExit:
			return
		default:
			if ia.Timestamp.After(iv.Timestamp) || ia.Timestamp.IsZero() {
				s.OnVideo(uint32(iv.Timestamp.Sub(vst).Milliseconds()), vp.(*VideoPack))
				if chase {
					if add10 := vst.Add(time.Millisecond * 10); realSt.After(add10) {
						vst = add10
					} else {
						vst = realSt
						chase = false
					}
				}
				iv, vp = vr.NextRead()
			} else {
				s.OnAudio(uint32(ia.Timestamp.Sub(vst).Milliseconds()), ap.(*AudioPack))
				ia, ap = ar.NextRead()
			}
		}
	}
}
func (s *Subscriber) onAudio(ts uint32, ap *AudioPack) {
	s.OnAudio(ts, ap)
}
func (s *Subscriber) onVideo(ts uint32, vp *VideoPack) {
	s.OnVideo(ts, vp)
}
func (s *Subscriber) PlayAudio(at *AudioTrack) {
	if s.Ctx2 != nil {
		at.Play(s.onAudio, s.Done(), s.Ctx2.Done())
	} else {
		at.Play(s.onAudio, s.Done(), nil)
	}
}
func (s *Subscriber) PlayVideo(vt *VideoTrack) {
	if s.Ctx2 != nil {
		vt.Play(s.onVideo, s.Done(), s.Ctx2.Done())
	} else {
		vt.Play(s.onVideo, s.Done(), nil)
	}
}
