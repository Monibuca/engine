package engine

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/Monibuca/engine/v4/track"
	"github.com/Monibuca/engine/v4/util"
	. "github.com/logrusorgru/aurora"
)

type StreamState byte
type StreamAction byte

const (
	STATE_WAITPUBLISH StreamState = iota // 等待发布者状态
	STATE_WAITTRACK                      // 等待Track
	STATE_PUBLISHING                     // 正在发布流状态
	STATE_WAITCLOSE                      // 等待关闭状态(自动关闭延时开启)
	STATE_CLOSED
)

const (
	ACTION_PUBLISH     StreamAction = iota
	ACTION_TIMEOUT                  // 发布流长时间没有数据/长时间没有发布者发布流/等待关闭时间到
	ACTION_PUBLISHLOST              // 发布者意外断开
	ACTION_CLOSE                    // 主动关闭流
	ACTION_LASTLEAVE                // 最后一个订阅者离开
	ACTION_FIRSTENTER               // 第一个订阅者进入
)

var StreamFSM = [STATE_CLOSED + 1]map[StreamAction]StreamState{
	{
		ACTION_PUBLISH:   STATE_WAITTRACK,
		ACTION_LASTLEAVE: STATE_CLOSED,
		ACTION_CLOSE:     STATE_CLOSED,
	},
	{
		ACTION_PUBLISHLOST: STATE_WAITPUBLISH,
		ACTION_TIMEOUT:     STATE_PUBLISHING,
		ACTION_CLOSE:       STATE_CLOSED,
	},
	{
		ACTION_PUBLISHLOST: STATE_WAITPUBLISH,
		ACTION_TIMEOUT:     STATE_WAITPUBLISH,
		ACTION_LASTLEAVE:   STATE_WAITCLOSE,
		ACTION_CLOSE:       STATE_CLOSED,
	},
	{
		ACTION_PUBLISHLOST: STATE_CLOSED,
		ACTION_TIMEOUT:     STATE_CLOSED,
		ACTION_FIRSTENTER:  STATE_PUBLISHING,
		ACTION_CLOSE:       STATE_CLOSED,
	},
	{},
}

// Streams 所有的流集合
var Streams = util.Map[string, *Stream]{Map: make(map[string]*Stream)}

type SubscribeAction *Subscriber
type UnSubscibeAction *Subscriber

// Stream 流定义
type Stream struct {
	context.Context
	cancel context.CancelFunc
	Publisher
	State       StreamState
	timeout     *time.Timer //当前状态的超时定时器
	actionChan  chan any
	Config      StreamConfig
	URL         string //远程地址，仅远程拉流有值
	StreamPath  string
	StartTime   time.Time               //流的创建时间
	Subscribers util.Slice[*Subscriber] // 订阅者
	Tracks
	FrameCount uint32 //帧总数
}

func (r *Stream) Register(streamPath string) (result bool) {
	if r == nil {
		r = &Stream{
			Config: config.StreamConfig,
		}
	}
	r.StreamPath = streamPath
	if result = Streams.Add(streamPath, r); result {
		r.actionChan = make(chan any, 1)
		r.StartTime = time.Now()
		r.timeout = time.NewTimer(r.Config.WaitTimeout.Duration())
		r.Context, r.cancel = context.WithCancel(Ctx)
		r.Init(r)
		go r.run()
	}
	return
}

// ForceRegister 强制注册流，会将已有的流踢掉
func (r *Stream) ForceRegister(streamPath string) {
	if ok := r.Register(streamPath); !ok {
		if s := Streams.Get(streamPath); s != nil {
			s.Close()
			<-s.Done()
		}
		r.ForceRegister(streamPath)
	} else {
		return
	}
}

func (r *Stream) action(action StreamAction) {
	if next, ok := StreamFSM[r.State][action]; ok {
		if r.Publisher == nil || r.OnStateChange(r.State, next) {
			util.Print(Yellow("Stream "), BrightCyan(r.StreamPath), " state changed :", r.State, "->", next)
			r.State = next
			switch next {
			case STATE_WAITPUBLISH:
				r.timeout.Reset(r.Config.WaitTimeout.Duration())
			case STATE_WAITTRACK:
				r.timeout.Reset(time.Second * 5)
			case STATE_PUBLISHING:
				r.WaitDone()
				r.timeout.Reset(r.Config.PublishTimeout.Duration())
			case STATE_WAITCLOSE:
				r.timeout.Reset(r.Config.WaitCloseTimeout.Duration())
			case STATE_CLOSED:
				r.cancel()
				r.WaitDone()
				close(r.actionChan)
				Streams.Delete(r.StreamPath)
				fallthrough
			default:
				r.timeout.Stop()
			}
		}
	}
}

func (r *Stream) Close() {
	r.actionChan <- ACTION_CLOSE
}
func (r *Stream) UnSubscribe(sub *Subscriber) {
	r.actionChan <- UnSubscibeAction(sub)
}
func (r *Stream) Subscribe(sub *Subscriber) {
	r.actionChan <- SubscribeAction(sub)
}
func (r *Stream) run() {
	for {
		select {
		case <-r.timeout.C:
			util.Print(Yellow("Stream "), BrightCyan(r.StreamPath), "timeout:", r.State)
			r.action(ACTION_TIMEOUT)
		case <-r.Done():
			r.action(ACTION_CLOSE)
		case action, ok := <-r.actionChan:
			if ok {
				switch v := action.(type) {
				case StreamAction:
					r.action(v)
				case SubscribeAction:
					v.Stream = r
					v.Context, v.cancel = context.WithCancel(r)
					r.Subscribers.Add(v)
					util.Print(Sprintf(Yellow("%s subscriber %s added remains:%d"), BrightCyan(r.StreamPath), Cyan(v.ID), Blue(len(r.Subscribers))))
					if r.Subscribers.Len() == 1 {
						r.action(ACTION_FIRSTENTER)
					}
				case UnSubscibeAction:
					if r.Subscribers.Delete(v) {
						util.Print(Sprintf(Yellow("%s subscriber %s removed remains:%d"), BrightCyan(r.StreamPath), Cyan(v.ID), Blue(len(r.Subscribers))))
						if r.Subscribers.Len() == 0 && r.Config.WaitCloseTimeout > 0 {
							r.action(ACTION_LASTLEAVE)
						}
					}
				}
			} else {
				return
			}
		}
	}
}

// Update 更新数据重置超时定时器
func (r *Stream) Update() uint32 {
	if r.State == STATE_PUBLISHING {
		r.timeout.Reset(r.Config.PublishTimeout.Duration())
	}
	return atomic.AddUint32(&r.FrameCount, 1)
}

// 如果暂时不知道编码格式可以用这个
func (r *Stream) NewVideoTrack() (vt *track.UnknowVideo) {
	vt = &track.UnknowVideo{
		Stream: r,
	}
	return
}

func (r *Stream) NewH264Track() (vt *track.H264) {
	return track.NewH264(r)
}

func (r *Stream) NewH265Track() (vt *track.H265) {
	return track.NewH265(r)
}

// func (r *Stream) WaitDataTrack(names ...string) DataTrack {
// 	t := <-r.WaitTrack(names...)
// 	return t.(DataTrack)
// }

func (r *Stream) WaitVideoTrack(names ...string) track.Video {
	if !r.Config.EnableVideo {
		return nil
	}
	t := <-r.WaitTrack(names...)
	return t.(track.Video)
}

func (r *Stream) WaitAudioTrack(names ...string) track.Audio {
	if !r.Config.EnableAudio {
		return nil
	}
	t := <-r.WaitTrack(names...)
	return t.(track.Audio)
}
