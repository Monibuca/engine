package engine

import (
	"context"
	"net/url"
	"sync"
	"time"

	. "github.com/Monibuca/engine/v4/common"
	"github.com/Monibuca/engine/v4/track"
	"github.com/pkg/errors"
)

type AudioFrame AVFrame[AudioSlice]
type VideoFrame AVFrame[NALUSlice]

// Subscriber 订阅者实体定义
type Subscriber struct {
	context.Context `json:"-"`
	cancel          context.CancelFunc
	*Stream         `json:"-"`
	ID              string
	TotalDrop       int //总丢帧
	TotalPacket     int
	Type            string
	BufferLength    int
	Delay           uint32
	SubscribeTime   time.Time
	SubscribeArgs   url.Values
	OnAudio         func(*AudioFrame) bool `json:"-"`
	OnVideo         func(*VideoFrame) bool `json:"-"`
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
	if stream := Streams.Get(streamPath); stream == nil {
		return errors.Errorf("subscribe %s faild :stream not found", streamPath)
	} else {
		if stream.Subscribe(s); s.Context == nil {
			return errors.Errorf("subscribe %s faild :stream closed", streamPath)
		}
	}
	return nil
}

//Play 开始播放
func (s *Subscriber) Play(at track.Audio, vt track.Video) {
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
	vr := vt.ReadRing() //从关键帧开始读取，首屏秒开
	ar := at.ReadRing()
	vp := vr.Read()
	ap := ar.TryRead()
	// chase := true
	for {
		if ap == nil && vp == nil {
			time.Sleep(time.Millisecond * 10)
		} else if ap != nil && (vp == nil || vp.SeqInStream > ap.SeqInStream) {
			s.onAudio(ap)
			ar.MoveNext()
		} else if vp != nil && (ap == nil || ap.SeqInStream > vp.SeqInStream) {
			s.onVideo(vp)
			// if chase {
			// 	if add10 := vst.Add(time.Millisecond * 10); realSt.After(add10) {
			// 		vst = add10
			// 	} else {
			// 		vst = realSt
			// 		chase = false
			// 	}
			// }
			vr.MoveNext()
		}
		ap = ar.TryRead()
		vp = vr.TryRead()
	}
}
func (s *Subscriber) onAudio(af *AVFrame[AudioSlice]) bool {
	return s.OnAudio((*AudioFrame)(af))
}
func (s *Subscriber) onVideo(vf *AVFrame[NALUSlice]) bool {
	return s.OnVideo((*VideoFrame)(vf))
}
func (s *Subscriber) PlayAudio(vt track.Audio) {
	vt.Play(s.onAudio)
}
func (s *Subscriber) PlayVideo(vt track.Video) {
	vt.Play(s.onVideo)
}
