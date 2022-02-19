package engine

import (
	"strings"
	"time"
	"unsafe"

	. "github.com/Monibuca/engine/v4/common"
	"github.com/Monibuca/engine/v4/log"
	"github.com/Monibuca/engine/v4/track"
	"github.com/Monibuca/engine/v4/util"
	. "github.com/logrusorgru/aurora"
	"go.uber.org/zap"
)

type StreamState byte
type StreamAction byte

type StateEvent struct {
	Action StreamAction
	From   StreamState
}

func (se StateEvent) Next() (next StreamState, ok bool) {
	next, ok = StreamFSM[se.From][se.Action]
	return
}

type SEwaitPublish struct {
	StateEvent
	Publisher IPublisher
}
type SEpublish struct {
	StateEvent
}
type SEwaitClose struct {
	StateEvent
}
type SEclose struct {
	StateEvent
}

type SEKick struct {
	Publisher IPublisher
}

const (
	STATE_WAITPUBLISH StreamState = iota // 等待发布者状态
	STATE_PUBLISHING                     // 正在发布流状态
	STATE_WAITCLOSE                      // 等待关闭状态(自动关闭延时开启)
	STATE_CLOSED                         // 流已关闭，不可使用
	STATE_DESTROYED                      // 资源已释放
)

const (
	ACTION_PUBLISH     StreamAction = iota
	ACTION_TIMEOUT                  // 发布流长时间没有数据/长时间没有发布者发布流/等待关闭时间到
	ACTION_PUBLISHLOST              // 发布者意外断开
	ACTION_CLOSE                    // 主动关闭流
	ACTION_LASTLEAVE                // 最后一个订阅者离开
	ACTION_FIRSTENTER               // 第一个订阅者进入
	ACTION_NOTRACKS                 // 轨道为空了
)

var StateNames = [...]string{"⌛", "🟢", "🟡", "🔴", "❌"}
var ActionNames = [...]string{"publish", "timeout", "publish lost", "close", "last leave", "first enter", "no tracks"}
var StreamFSM = [STATE_DESTROYED + 1]map[StreamAction]StreamState{
	{
		ACTION_PUBLISH:   STATE_PUBLISHING,
		ACTION_TIMEOUT:   STATE_CLOSED,
		ACTION_LASTLEAVE: STATE_CLOSED,
		ACTION_CLOSE:     STATE_CLOSED,
	},
	{
		ACTION_PUBLISHLOST: STATE_WAITPUBLISH,
		ACTION_NOTRACKS:    STATE_WAITPUBLISH,
		ACTION_LASTLEAVE:   STATE_WAITCLOSE,
		ACTION_CLOSE:       STATE_CLOSED,
	},
	{
		ACTION_PUBLISHLOST: STATE_CLOSED,
		ACTION_TIMEOUT:     STATE_CLOSED,
		ACTION_FIRSTENTER:  STATE_PUBLISHING,
		ACTION_CLOSE:       STATE_CLOSED,
	},
	{
		ACTION_TIMEOUT: STATE_DESTROYED,
	},
	{},
}

// Streams 所有的流集合
var Streams = util.Map[string, *Stream]{Map: make(map[string]*Stream)}

func FilterStreams[T IPublisher]() (ss []*Stream) {
	Streams.RLock()
	defer Streams.RUnlock()
	for _, s := range Streams.Map {
		if _, ok := s.Publisher.(T); ok {
			ss = append(ss, s)
		}
	}
	return
}

type StreamTimeoutConfig struct {
	WaitTimeout      time.Duration
	PublishTimeout   time.Duration
	WaitCloseTimeout time.Duration
}

// Stream 流定义
type Stream struct {
	*zap.Logger
	StartTime time.Time //创建时间
	StreamTimeoutConfig
	Path        string
	Publisher   IPublisher
	State       StreamState
	timeout     *time.Timer //当前状态的超时定时器
	actionChan  chan any
	Subscribers util.Slice[ISubscriber] // 订阅者
	Tracks      map[string]Track
	AppName     string
	StreamName  string
}

func (s *Stream) SSRC() uint32 {
	return uint32(uintptr(unsafe.Pointer(s)))
}

func findOrCreateStream(streamPath string, waitTimeout time.Duration) (s *Stream, created bool) {
	p := strings.Split(streamPath, "/")
	if len(p) < 2 {
		log.Warn(Red("Stream Path Format Error:"), streamPath)
		return nil, false
	}
	if s, ok := Streams.Map[streamPath]; ok {
		s.Debug("Stream Found")
		return s, false
	} else {
		p := strings.Split(streamPath, "/")
		s = &Stream{
			Path:       streamPath,
			AppName:    p[0],
			StreamName: util.LastElement(p),
		}
		s.Logger = log.With(zap.String("stream", streamPath))
		s.Info("created")
		s.WaitTimeout = waitTimeout
		Streams.Map[streamPath] = s
		s.actionChan = make(chan any, 1)
		s.timeout = time.NewTimer(waitTimeout)
		s.Tracks = make(map[string]Track)
		go s.run()
		return s, true
	}
}
func (r *Stream) broadcast(event any) {
	for _, sub := range r.Subscribers {
		sub.OnEvent(event)
	}
}
func (r *Stream) action(action StreamAction) (ok bool) {
	event := StateEvent{From: r.State, Action: action}
	if r.State, ok = event.Next(); ok {
		// 给Publisher状态变更的回调，方便进行远程拉流等操作
		var stateEvent any
		r.Debug(Sprintf("%s%s%s", StateNames[event.From], Yellow("->"), StateNames[r.State]), zap.String("action", ActionNames[action]))
		switch r.State {
		case STATE_WAITPUBLISH:
			stateEvent = SEwaitPublish{event, r.Publisher}
			Bus.Publish(Event_REQUEST_PUBLISH, r)
			r.timeout.Reset(r.WaitTimeout)
			if _, ok = PullOnSubscribeList[r.Path]; ok {
				PullOnSubscribeList[r.Path].Pull()
			}
		case STATE_PUBLISHING:
			stateEvent = SEpublish{event}
			r.broadcast(stateEvent)
			r.timeout.Reset(time.Second * 5) // 5秒心跳，检测track的存活度
			Bus.Publish(Event_PUBLISH, r)
			if v, ok := PushOnPublishList[r.Path]; ok {
				for _, v := range v {
					v.Push()
				}
			}
		case STATE_WAITCLOSE:
			stateEvent = SEwaitClose{event}
			r.timeout.Reset(r.WaitCloseTimeout)
		case STATE_CLOSED:
			stateEvent = SEclose{event}
			r.broadcast(stateEvent)
			r.Subscribers.Reset()
			Bus.Publish(Event_STREAMCLOSE, r)
			Streams.Delete(r.Path)
			r.timeout.Reset(time.Second) // 延迟1秒钟销毁，防止访问到已关闭的channel
		case STATE_DESTROYED:
			close(r.actionChan)
			fallthrough
		default:
			r.timeout.Stop()
		}
		if r.Publisher != nil {
			r.Publisher.OnEvent(stateEvent)
		}
	}
	return
}
func (r *Stream) IsClosed() bool {
	if r == nil {
		return true
	}
	return r.State >= STATE_CLOSED
}

func (s *Stream) Close() {
	s.Receive(ACTION_CLOSE)
}

func (s *Stream) Receive(event any) {
	if !s.IsClosed() {
		s.actionChan <- event
	}
}

// 流状态处理中枢，包括接收订阅发布指令等
func (s *Stream) run() {
	for {
		select {
		case <-s.timeout.C:
			if s.State == STATE_PUBLISHING {
				for name, t := range s.Tracks {
					// track 超过一定时间没有更新数据了
					if lastWriteTime := t.LastWriteTime(); !lastWriteTime.IsZero() && time.Since(lastWriteTime) > s.PublishTimeout {
						s.Warn("track timeout", zap.String("name", name), zap.Time("lastWriteTime", lastWriteTime), zap.Duration("timeout", s.PublishTimeout))
						delete(s.Tracks, name)
						s.broadcast(TrackRemoved(t))
					}
				}
				if len(s.Tracks) == 0 {
					s.action(ACTION_NOTRACKS)
				} else {
					s.timeout.Reset(time.Second * 5)
				}
			} else {
				s.Debug("timeout", zap.String("state", StateNames[s.State]))
				s.action(ACTION_TIMEOUT)
			}
		case action, ok := <-s.actionChan:
			if ok {
				switch v := action.(type) {
				case IPublisher:
					if v.IsClosed() {
						s.action(ACTION_PUBLISHLOST)
					} else if s.action(ACTION_PUBLISH) {
						s.Publisher = v
						v.OnEvent(s) // 通知Publisher已成功进入Stream
					}
				case Track:
					name := v.GetName()
					if _, ok := s.Tracks[name]; !ok {
						s.Tracks[name] = v
						s.Info("TrackAdd", zap.String("name", name))
						for _, sub := range s.Subscribers {
							sub.OnEvent(v) // 通知Subscriber有新Track可用了
						}
					}
				case TrackRemoved:
					name := v.GetName()
					if _, ok := s.Tracks[name]; ok {
						delete(s.Tracks, name)
						for _, sub := range s.Subscribers {
							sub.OnEvent(v) // 通知Subscriber Track已被移除
						}
						if len(s.Tracks) == 0 {
							s.action(ACTION_NOTRACKS)
						}
					}
				case StreamAction:
					s.action(v)
				case ISubscriber:
					if !v.IsClosed() {
						s.Subscribers.Add(v)
						if wt := v.GetSubscribeConfig().WaitTimeout.Duration(); wt > s.WaitTimeout {
							s.WaitTimeout = wt
						}
						v.OnEvent(s) // 通知Subscriber已成功进入Stream
						Bus.Publish(Event_SUBSCRIBE, v)
						s.Info("suber added", zap.String("id", v.getID()), zap.String("type", v.getType()), zap.Int("remains", len(s.Subscribers)))
						if s.Publisher != nil {
							s.Publisher.OnEvent(v) // 通知Publisher有新的订阅者加入，在回调中可以去获取订阅者数量
						}
						if s.Subscribers.Len() == 1 {
							s.action(ACTION_FIRSTENTER)
						}
					} else if s.Subscribers.Delete(v) {
						Bus.Publish(Event_UNSUBSCRIBE, v)
						s.Info("suber removed", zap.String("id", v.getID()), zap.String("type", v.getType()), zap.Int("remains", len(s.Subscribers)))
						if s.Publisher != nil {
							s.Publisher.OnEvent(v) // 通知Publisher有订阅者离开，在回调中可以去获取订阅者数量
						}
						if s.Subscribers.Len() == 0 && s.WaitCloseTimeout > 0 {
							s.action(ACTION_LASTLEAVE)
						}
					}
				}
			} else {
				return
			}
			// default:

		}
	}
}

func (s *Stream) AddTrack(t Track) {
	s.Receive(t)
}

type TrackRemoved Track

func (s *Stream) RemoveTrack(t Track) {
	s.Receive(TrackRemoved(t))
}

// 如果暂时不知道编码格式可以用这个
func (r *Stream) NewVideoTrack() (vt *track.UnknowVideo) {
	r.Debug("create unknow video track")
	vt = &track.UnknowVideo{}
	vt.Stream = r
	return
}
func (r *Stream) NewAudioTrack() (at *track.UnknowAudio) {
	r.Debug("create unknow audio track")
	at = &track.UnknowAudio{}
	at.Stream = r
	return
}

// func (r *Stream) WaitDataTrack(names ...string) DataTrack {
// 	t := <-r.WaitTrack(names...)
// 	return t.(DataTrack)
// }
