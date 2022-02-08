package engine

import (
	"context"
	"net/url"
	"time"

	. "github.com/Monibuca/engine/v4/common"
	"github.com/Monibuca/engine/v4/config"
	"github.com/Monibuca/engine/v4/track"
)

type AudioFrame AVFrame[AudioSlice]
type VideoFrame AVFrame[NALUSlice]

// Subscriber 订阅者实体定义
type Subscriber struct {
	context.Context `json:"-"`
	cancel          context.CancelFunc
	Config          config.Subscribe
	Stream          *Stream `json:"-"`
	ID              string
	TotalDrop       int //总丢帧
	TotalPacket     int
	Type            string
	BufferLength    int
	Delay           uint32
	SubscribeTime   time.Time
	SubscribeArgs   url.Values
	OnAudio         func(*AudioFrame) error `json:"-"`
	OnVideo         func(*VideoFrame) error `json:"-"`
}

// Close 关闭订阅者
func (s *Subscriber) Close() {
	s.Stream.UnSubscribe(s)
	if s.cancel != nil {
		s.cancel()
	}
}

//Subscribe 开始订阅 将Subscriber与Stream关联
func (sub *Subscriber) Subscribe(streamPath string, config config.Subscribe) bool {
	Streams.Lock()
	defer Streams.Unlock()
	s, created := findOrCreateStream(streamPath, config.WaitTimeout.Duration())
	if s.IsClosed() {
		return false
	}
	if created {
		Bus.Publish(Event_REQUEST_PUBLISH, s)
		go s.run()
	}
	if s.Subscribe(sub); sub.Stream != nil {
		sub.Config = config
	}
	return true
}

//Play 开始播放
func (s *Subscriber) Play(at *track.Audio, vt *track.Video) {
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
	for s.Err() == nil {
		if ap == nil && vp == nil {
			time.Sleep(time.Millisecond * 10)
		} else if ap != nil && (vp == nil || vp.SeqInStream > ap.SeqInStream) {
			if s.onAudio(ap) != nil {
				return
			}
			ar.MoveNext()
		} else if vp != nil && (ap == nil || ap.SeqInStream > vp.SeqInStream) {
			if s.onVideo(vp) != nil {
				return
			}
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
func (s *Subscriber) onAudio(af *AVFrame[AudioSlice]) error {
	return s.OnAudio((*AudioFrame)(af))
}
func (s *Subscriber) onVideo(vf *AVFrame[NALUSlice]) error {
	return s.OnVideo((*VideoFrame)(vf))
}
func (s *Subscriber) PlayAudio(at *track.Audio) {
	at.Play(s.onAudio)
}
func (s *Subscriber) PlayVideo(vt *track.Video) {
	vt.Play(s.onVideo)
}
func (r *Subscriber) WaitVideoTrack(names ...string) *track.Video {
	if !r.Config.SubVideo {
		return nil
	}
	if len(names) == 0 {
		names = []string{"h264", "h265"}
	}
	if t := <-r.Stream.WaitTrack(names...); t == nil {
		return nil
	} else {
		switch vt := t.(type) {
		case *track.H264:
			return (*track.Video)(vt)
		case *track.H265:
			return (*track.Video)(vt)
		}
		return nil
	}
}

func (r *Subscriber) WaitAudioTrack(names ...string) *track.Audio {
	if !r.Config.SubAudio {
		return nil
	}
	if len(names) == 0 {
		names = []string{"aac", "pcma", "pcmu"}
	}
	if t := <-r.Stream.WaitTrack(names...); t == nil {
		return nil
	} else {
		switch at := t.(type) {
		case *track.AAC:
			return (*track.Audio)(at)
		case *track.G711:
			return (*track.Audio)(at)
		}
		return nil
	}
}
