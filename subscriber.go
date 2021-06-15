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
	ctx2 := s.Ctx2
	if ctx2 == nil {
		ctx2 = context.TODO()
	}
	select {
	case <-vt.WaitIDR.Done(): //等待获取到第一个关键帧
	case <-s.Context.Done():
		return
	case <-ctx2.Done(): //可能等不到关键帧就退出了
		return
	}
	vr := vt.Buffer.SubRing(vt.IDRIndex) //从关键帧开始读取，首屏秒开
	vr.Current.Wait()                    //等到RingBuffer可读
	ar := at.Buffer.SubRing(at.Buffer.Index)
	ar.Current.Wait()
	dropping := false //是否处于丢帧中
	startTimestamp := vr.Current.Timestamp
	for ctx2.Err() == nil && s.Context.Err() == nil {
		if ar.Current.Timestamp > vr.Current.Timestamp || ar.Current.Timestamp == 0 {
			if !dropping {
				s.OnVideo(vr.Current.VideoPack.Copy(startTimestamp))
				if vt.Buffer.Index-vr.Index > 128 {
					dropping = true
				}
			} else if vr.Current.IDR {
				dropping = false
			}
			if !vr.NextR() {
				return
			}
		} else {
			if !dropping {
				s.OnAudio(ar.Current.AudioPack.Copy(startTimestamp))
				if at.Buffer.Index-ar.Index > 128 {
					dropping = true
				}
			}
			if !ar.NextR() {
				return
			}
		}
	}
}
func (s *Subscriber) PlayAudio(at *AudioTrack) {
	ring := at.Buffer.SubRing(at.Buffer.Index)
	ring.Current.Wait()
	startTimestamp := ring.Current.Timestamp
	droped := 0
	var action, send func()
	drop := func() {
		if at.Buffer.Index-ring.Index < 10 {
			action = send
		} else {
			droped++
		}
	}
	send = func() {
		s.OnAudio(ring.Current.AudioPack.Copy(startTimestamp))

		//s.BufferLength = pIndex - ring.Index
		//s.Delay = s.AVRing.Timestamp - packet.Timestamp
		if at.Buffer.Index-ring.Index > 128 {
			action = drop
		}
	}
	ctx2 := s.Ctx2
	if ctx2 == nil {
		ctx2 = context.TODO()
	}
	action = send
	for running := true; ctx2.Err() == nil && s.Context.Err() == nil && running; running = ring.NextR() {
		action()
	}
}

func (s *Subscriber) PlayVideo(vt *VideoTrack) {
	ctx2 := s.Ctx2
	if ctx2 == nil {
		ctx2 = context.TODO()
	}
	select {
	case <-vt.WaitIDR.Done():
	case <-s.Context.Done():
		return
	case <-ctx2.Done(): //可能等不到关键帧就退出了
		return
	}
	ring := vt.Buffer.SubRing(vt.IDRIndex)
	ring.Current.Wait()
	startTimestamp := ring.Current.Timestamp
	droped := 0
	var action, send func()
	drop := func() {
		if ring.Current.IDR {
			action = send
		} else {
			droped++
		}
	}
	send = func() {
		s.OnVideo(ring.Current.VideoPack.Copy(startTimestamp))
		pIndex := vt.Buffer.Index
		//s.BufferLength = pIndex - ring.Index
		//s.Delay = s.AVRing.Timestamp - packet.Timestamp
		if pIndex-ring.Index > 128 {
			action = drop
		}
	}
	action = send
	for running := true; ctx2.Err() == nil && s.Context.Err() == nil && running; running = ring.NextR() {
		action()
	}
}
