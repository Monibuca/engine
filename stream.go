package engine

import (
	"errors"
	"strings"
	"sync"
	"time"
	"unsafe"

	. "github.com/logrusorgru/aurora"
	"go.uber.org/zap"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/log"
	"m7s.live/engine/v4/track"
	"m7s.live/engine/v4/util"
)

type StreamState byte
type StreamAction byte

type StateEvent struct {
	Action StreamAction
	From   StreamState
	Stream *Stream
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
}

// 四状态机
const (
	STATE_WAITPUBLISH StreamState = iota // 等待发布者状态
	STATE_PUBLISHING                     // 正在发布流状态
	STATE_WAITCLOSE                      // 等待关闭状态(自动关闭延时开启)
	STATE_CLOSED                         // 流已关闭，不可使用
)

const (
	ACTION_PUBLISH     StreamAction = iota
	ACTION_TIMEOUT                  // 发布流长时间没有数据/长时间没有发布者发布流/等待关闭时间到
	ACTION_PUBLISHLOST              // 发布者意外断开
	ACTION_CLOSE                    // 主动关闭流
	ACTION_LASTLEAVE                // 最后一个订阅者离开
	ACTION_FIRSTENTER               // 第一个订阅者进入
)

var StateNames = [...]string{"⌛", "🟢", "🟡", "🔴"}
var ActionNames = [...]string{"publish", "timeout", "publish lost", "close", "last leave", "first enter", "no tracks"}

/*
stateDiagram-v2
    [*] --> ⌛等待发布者 : 创建
    ⌛等待发布者 --> 🟢正在发布 :发布
    ⌛等待发布者 --> 🔴已关闭 :关闭
    ⌛等待发布者 --> 🔴已关闭  :超时
    ⌛等待发布者 --> 🔴已关闭  :最后订阅者离开
    🟢正在发布 --> ⌛等待发布者: 发布者断开
    🟢正在发布 --> 🟡等待关闭: 最后订阅者离开
    🟢正在发布 --> 🔴已关闭  :关闭
    🟡等待关闭 --> 🟢正在发布 :第一个订阅者进入
    🟡等待关闭 --> 🔴已关闭  :关闭
    🟡等待关闭 --> 🔴已关闭  :超时
    🟡等待关闭 --> 🔴已关闭  :发布者断开
*/

var StreamFSM = [len(StateNames)]map[StreamAction]StreamState{
	{
		ACTION_PUBLISH:   STATE_PUBLISHING,
		ACTION_TIMEOUT:   STATE_CLOSED,
		ACTION_LASTLEAVE: STATE_CLOSED,
		ACTION_CLOSE:     STATE_CLOSED,
	},
	{
		ACTION_PUBLISHLOST: STATE_WAITPUBLISH,
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
	timeout    *time.Timer //当前状态的超时定时器
	actionChan util.SafeChan[any]
	*zap.Logger
	StartTime time.Time //创建时间
	StreamTimeoutConfig
	Path        string
	Publisher   IPublisher
	State       StreamState
	Subscribers []ISubscriber // 订阅者
	Tracks      map[string]Track
	AppName     string
	StreamName  string
}
type StreamSummay struct {
	Path        string
	State       StreamState
	Subscribers int
	Tracks      []string
	StartTime   time.Time
	Type        string
	BPS         int
}

// Summary 返回流的简要信息
func (s *Stream) Summary() (r StreamSummay) {
	if s.Publisher != nil {
		r.Type = s.Publisher.GetIO().Type
	}
	//TODO: Lock
	for _, t := range s.Tracks {
		r.BPS += t.GetBase().BPS
		r.Tracks = append(r.Tracks, t.GetBase().Name)
	}
	r.Path = s.Path
	r.State = s.State
	r.Subscribers = len(s.Subscribers)
	r.StartTime = s.StartTime
	return
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
		s.actionChan.Init(1)
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
	event := StateEvent{action, r.State, r}
	var next StreamState
	if next, ok = event.Next(); ok {
		r.State = next
		// 给Publisher状态变更的回调，方便进行远程拉流等操作
		var stateEvent any
		r.Debug(Sprintf("%s%s%s", StateNames[event.From], Yellow("->"), StateNames[next]), zap.String("action", ActionNames[action]))
		switch next {
		case STATE_WAITPUBLISH:
			stateEvent = SEwaitPublish{event, r.Publisher}
			r.timeout.Reset(r.WaitTimeout)
		case STATE_PUBLISHING:
			stateEvent = SEpublish{event}
			r.broadcast(stateEvent)
			r.timeout.Reset(time.Second * 5) // 5秒心跳，检测track的存活度
		case STATE_WAITCLOSE:
			stateEvent = SEwaitClose{event}
			r.timeout.Reset(r.WaitCloseTimeout)
		case STATE_CLOSED:
			for !r.actionChan.Close() {
				// 等待channel发送完毕
				time.Sleep(time.Millisecond * 100)
			}
			stateEvent = SEclose{event}
			r.broadcast(stateEvent)
			r.Subscribers = nil
			Streams.Delete(r.Path)
			r.timeout.Stop()
		}
		EventBus <- stateEvent
		if r.Publisher != nil {
			r.Publisher.OnEvent(stateEvent)
		}
	} else {
		r.Debug("wrong action", zap.String("action", ActionNames[action]))
	}
	return
}
func (r *Stream) IsClosed() bool {
	if r == nil {
		return true
	}
	return r.State == STATE_CLOSED
}

func (s *Stream) Close() {
	s.Receive(ACTION_CLOSE)
}

func (s *Stream) Receive(event any) bool {
	return s.actionChan.Send(event)
}

// 流状态处理中枢，包括接收订阅发布指令等
func (s *Stream) run() {
	waitP := make(map[*util.Promise[ISubscriber, struct{}]]byte)
	for {
		select {
		case <-s.timeout.C:
			if s.State == STATE_PUBLISHING {
				for name, t := range s.Tracks {
					// track 超过一定时间没有更新数据了
					if lastWriteTime := t.LastWriteTime(); !lastWriteTime.IsZero() && time.Since(lastWriteTime) > s.PublishTimeout {
						s.Warn("track timeout", zap.String("name", name), zap.Time("lastWriteTime", lastWriteTime), zap.Duration("timeout", s.PublishTimeout))
						delete(s.Tracks, name)
						s.broadcast(TrackRemoved{t})
					}
				}
				for l := len(s.Subscribers) - 1; l >= 0; l-- {
					if sub := s.Subscribers[l]; sub.IsClosed() {
						s.Subscribers = append(s.Subscribers[:l], s.Subscribers[l+1:]...)
						s.Info("suber -1", zap.String("id", sub.GetIO().ID), zap.String("type", sub.GetIO().Type), zap.Int("remains", len(s.Subscribers)))
						if s.Publisher != nil {
							s.Publisher.OnEvent(sub) // 通知Publisher有订阅者离开，在回调中可以去获取订阅者数量
						}
						if l == 0 && s.WaitCloseTimeout > 0 {
							s.action(ACTION_LASTLEAVE)
						}
					}
				}
				if len(s.Tracks) == 0 || (s.Publisher != nil && s.Publisher.IsClosed()) {
					s.action(ACTION_PUBLISHLOST)
					for p := range waitP {
						p.Reject(errors.New("publisher lost"))
						delete(waitP, p)
					}
				} else {
					s.timeout.Reset(time.Second * 5)
					//订阅者等待音视频轨道超时了，放弃等待，订阅成功
					for p := range waitP {
						p.Resolve(util.Null)
						delete(waitP, p)
					}
				}
			} else {
				s.Debug("timeout", zap.String("state", StateNames[s.State]))
				s.action(ACTION_TIMEOUT)
			}
		case action, ok := <-s.actionChan.C:
			if ok {
				switch v := action.(type) {
				case *util.Promise[IPublisher, struct{}]:
					s.Publisher = v.Value
					if s.action(ACTION_PUBLISH) {
						io := v.Value.GetIO()
						io.Spesic = v.Value
						io.Stream = s
						io.StartTime = time.Now()
						io.Logger = s.With(zap.String("type", io.Type))
						if io.ID != "" {
							io.Logger = io.Logger.With(zap.String("ID", io.ID))
						}
						v.Resolve(util.Null)
					} else {
						s.Publisher = nil
						v.Reject(BadNameErr)
					}
				case *util.Promise[ISubscriber, struct{}]:
					if s.IsClosed() {
						v.Reject(StreamIsClosedErr)
					}
					suber := v.Value
					io := suber.GetIO()
					io.Spesic = suber
					s.Subscribers = append(s.Subscribers, suber)
					sbConfig := io.Config
					if wt := util.Second2Duration(sbConfig.WaitTimeout); wt > s.WaitTimeout {
						s.WaitTimeout = wt
					}
					io.Stream = s
					io.StartTime = time.Now()
					io.Logger = s.With(zap.String("type", io.Type))
					if io.ID != "" {
						io.Logger = io.Logger.With(zap.String("ID", io.ID))
					}
					s.Info("suber +1", zap.String("id", io.ID), zap.String("type", io.Type), zap.Int("remains", len(s.Subscribers)))
					if s.Publisher != nil {
						s.Publisher.OnEvent(v) // 通知Publisher有新的订阅者加入，在回调中可以去获取订阅者数量
						needAudio, needVideo := sbConfig.SubAudio && s.Publisher.GetConfig().PubAudio, sbConfig.SubVideo && s.Publisher.GetConfig().PubVideo
						for _, t := range s.Tracks {
							switch t.(type) {
							case *track.Audio:
								if needAudio {
									needAudio = false
								} else {
									continue
								}
							case *track.Video:
								if needVideo {
									needVideo = false
								} else {
									continue
								}
							}
							suber.OnEvent(t) // 把现有的Track发给订阅者
						}
						// 还需要等一下发布者的音频或者视频Track
						if needAudio || needVideo {
							waitP[v] = 0
							if needAudio {
								waitP[v] |= 2
							}
							if needVideo {
								waitP[v] |= 1
							}
						} else {
							v.Resolve(util.Null)
						}
					} else {
						waitP[v] = 3
					}
					if len(s.Subscribers) == 1 {
						s.action(ACTION_FIRSTENTER)
					}
				case Track:
					name := v.GetBase().Name
					if _, ok := s.Tracks[name]; !ok {
						s.Tracks[name] = v
						s.Info("track +1", zap.String("name", name))
						s.broadcast(v)
						for w, flag := range waitP {
							if _, ok := v.(*track.Audio); ok && (flag&2) != 0 {
								flag = flag &^ 2
							}
							if _, ok := v.(*track.Video); ok && (flag&1) != 0 {
								flag = flag &^ 1
							}
							if flag == 0 {
								w.Resolve(util.Null)
								delete(waitP, w)
							} else {
								waitP[w] = flag
							}
						}
					}
				case TrackRemoved:
					name := v.GetBase().Name
					if t, ok := s.Tracks[name]; ok {
						s.Info("track -1", zap.String("name", name))
						delete(s.Tracks, name)
						s.broadcast(v)
						if len(s.Tracks) == 0 {
							s.action(ACTION_PUBLISHLOST)
						}
						if dt, ok := t.(*track.Data); ok {
							dt.Dispose()
						}
					}
				case StreamAction:
					s.action(v)
				default:
					s.Error("unknown action", zap.Any("action", action))
				}
			} else {
				for w := range waitP {
					w.Reject(StreamIsClosedErr)
				}
				for _, t := range s.Tracks {
					if dt, ok := t.(*track.Data); ok {
						dt.Dispose()
					}
				}
				return
			}
		}
	}
}

func (s *Stream) AddTrack(t Track) {
	s.Receive(t)
}

type TrackRemoved struct {
	Track
}

func (s *Stream) RemoveTrack(t Track) {
	s.Receive(TrackRemoved{t})
}

func (r *Stream) NewDataTrack(locker sync.Locker) (dt *track.Data) {
	dt = &track.Data{
		Locker: locker,
	}
	dt.Init(10)
	dt.Stream = r
	return
}
