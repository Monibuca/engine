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
	if u, err := url.Parse(streamPath); err != nil {
		return err
	} else if s.SubscribeArgs, err = url.ParseQuery(u.RawQuery); err != nil {
		return err
	} else {
		streamPath = u.Path
	}
	var stream *Stream = nil
	if stream = FindStream(streamPath); stream == nil {
		if config.OnDemandPublishTimeout > 0 {
			//TriggerHook(HOOK_ONDEMAND_PUBLISH, streamPath)
			stream = WaitStream(streamPath, config.OnDemandPublishTimeout)
		} else {
			return errors.Errorf("subscribe %s faild :stream not found", streamPath)
		}

	}

	if stream != nil {
		if stream.Subscribe(s); s.Context == nil {
			return errors.Errorf("subscribe %s faild :stream closed", streamPath)
		}
	} else {
		return errors.Errorf("subscribe %s faild :stream not found", streamPath)
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
	ia, ap := ar.TryRead()
	vst := iv.Timestamp
	chase := true
	for {
		select {
		case <-extraExit:
			return
		case <-streamExit:
			return
		default:
			if ia == nil && iv == nil {
				time.Sleep(time.Millisecond * 10)
			} else if ia != nil && (iv == nil || iv.Timestamp.After(ia.Timestamp)) {
				s.OnAudio(uint32(ia.Timestamp.Sub(vst).Milliseconds()), ap.(*AudioPack))
				ar.MoveNext()
			} else if iv != nil && (ia == nil || ia.Timestamp.After(iv.Timestamp)) {
				s.OnVideo(uint32(iv.Timestamp.Sub(vst).Milliseconds()), vp.(*VideoPack))
				if chase {
					if add10 := vst.Add(time.Millisecond * 10); realSt.After(add10) {
						vst = add10
					} else {
						vst = realSt
						chase = false
					}
				}
				vr.MoveNext()
			}
			ia, ap = ar.TryRead()
			iv, vp = vr.TryRead()
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
