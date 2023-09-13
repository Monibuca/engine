package engine

import (
	"encoding/json"
	"strings"
	"sync"
	"time"
	"unsafe"

	. "github.com/logrusorgru/aurora"
	"go.uber.org/zap"
	"m7s.live/engine/v4/common"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/config"
	"m7s.live/engine/v4/log"
	"m7s.live/engine/v4/track"
	"m7s.live/engine/v4/util"
)

type StreamState byte
type StreamAction byte

func (s StreamState) String() string {
	return StateNames[s]
}
func (s StreamAction) String() string {
	return ActionNames[s]
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
var Streams util.Map[string, *Stream]

func FilterStreams[T IPublisher]() (ss []*Stream) {
	Streams.Range(func(_ string, s *Stream) {
		if _, ok := s.Publisher.(T); ok {
			ss = append(ss, s)
		}
	})
	return
}

type StreamTimeoutConfig struct {
	PublishTimeout    time.Duration //发布者无数据后超时
	DelayCloseTimeout time.Duration //无订阅者后超时,必须先有一次订阅才会激活
	IdleTimeout       time.Duration //无订阅者后超时，不需要订阅即可激活
	PauseTimeout      time.Duration //暂停后超时
	NeverTimeout      bool          // 永不超时
}
type Tracks struct {
	sync.Map
	MainVideo   *track.Video
	SEI         *track.Data[[]byte]
	marshalLock sync.Mutex
}

func (tracks *Tracks) Range(f func(name string, t Track)) {
	tracks.Map.Range(func(k, v any) bool {
		f(k.(string), v.(Track))
		return true
	})
}

func (tracks *Tracks) Add(name string, t Track) bool {
	switch v := t.(type) {
	case *track.Video:
		if tracks.MainVideo == nil {
			tracks.MainVideo = v
			tracks.SetIDR(v)
		}
		if tracks.SEI != nil {
			v.SEIReader = &track.DataReader[[]byte]{}
			v.SEIReader.Ring = tracks.SEI.Ring
		}
	case *track.Audio:
		if tracks.MainVideo != nil {
			v.Narrow()
		}
	}
	_, loaded := tracks.LoadOrStore(name, t)
	return !loaded
}

func (tracks *Tracks) SetIDR(video Track) {
	if video == tracks.MainVideo {
		tracks.Range(func(_ string, t Track) {
			if v, ok := t.(*track.Audio); ok {
				v.Narrow()
			}
		})
	}
}

func (tracks *Tracks) AddSEI(t byte, data []byte) bool {
	if tracks.SEI != nil {
		l := len(data)
		var buffer util.Buffer
		buffer.WriteByte(t)
		for l >= 255 {
			buffer.WriteByte(255)
			l -= 255
		}
		buffer.WriteByte(byte(l))
		buffer.Write(data)
		buffer.WriteByte(0x80)
		tracks.SEI.Push(buffer)
		return true
	}
	return false
}

func (tracks *Tracks) MarshalJSON() ([]byte, error) {
	var trackList []Track
	tracks.marshalLock.Lock()
	defer tracks.marshalLock.Unlock()
	tracks.Range(func(_ string, t Track) {
		t.SnapForJson()
		trackList = append(trackList, t)
	})
	return json.Marshal(trackList)
}

// Stream 流定义
type Stream struct {
	timeout    *time.Timer //当前状态的超时定时器
	actionChan util.SafeChan[any]
	*log.Logger
	StartTime time.Time //创建时间
	StreamTimeoutConfig
	Path        string
	Publisher   IPublisher
	State       StreamState
	SEHistory   []StateEvent // 事件历史
	Subscribers Subscribers  // 订阅者
	Tracks      Tracks
	AppName     string
	StreamName  string
	IsPause     bool // 是否处于暂停状态
	pubLocker   sync.Mutex
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

func (s *Stream) GetType() string {
	if s.Publisher == nil {
		return ""
	}
	return s.Publisher.GetPublisher().Type
}

func (s *Stream) GetStartTime() time.Time {
	return s.StartTime
}

func (s *Stream) GetPublisherConfig() *config.Publish {
	if s.Publisher == nil {
		s.Error("GetPublisherConfig: Publisher is nil")
		return nil
	}
	return s.Publisher.GetPublisher().Config
}

// Summary 返回流的简要信息
func (s *Stream) Summary() (r StreamSummay) {
	if s.Publisher != nil {
		r.Type = s.Publisher.GetPublisher().Type
	}
	s.Tracks.Range(func(name string, t Track) {
		r.BPS += t.GetBPS()
		r.Tracks = append(r.Tracks, name)
	})
	r.Path = s.Path
	r.State = s.State
	r.Subscribers = s.Subscribers.Len()
	r.StartTime = s.StartTime
	return
}

func (s *Stream) SSRC() uint32 {
	return uint32(uintptr(unsafe.Pointer(s)))
}
func (s *Stream) SetIDR(video Track) {
	s.Tracks.SetIDR(video)
}
func findOrCreateStream(streamPath string, waitTimeout time.Duration) (s *Stream, created bool) {
	p := strings.Split(streamPath, "/")
	if len(p) < 2 {
		log.Warn(Red("Stream Path Format Error:"), streamPath)
		return nil, false
	}
	actual, loaded := Streams.LoadOrStore(streamPath, &Stream{
		Path:       streamPath,
		AppName:    p[0],
		StreamName: strings.Join(p[1:], "/"),
		StartTime:  time.Now(),
		Logger:     log.LocaleLogger.With(zap.String("stream", streamPath)),
		timeout:    time.NewTimer(waitTimeout),
	})
	if s := actual.(*Stream); loaded {
		s.Debug("Stream Found")
		return s, false
	} else {
		s.Subscribers.Init()
		s.actionChan.Init(1)
		s.Info("created")
		go s.run()
		return s, true
	}
}

func (r *Stream) action(action StreamAction) (ok bool) {
	var event StateEvent
	event.Target = r
	event.Action = action
	event.From = r.State
	event.Time = time.Now()
	var next StreamState
	if next, ok = event.Next(); ok {
		r.State = next
		r.SEHistory = append(r.SEHistory, event)
		// 给Publisher状态变更的回调，方便进行远程拉流等操作
		var stateEvent any
		r.Info(Sprintf("%s%s%s", event.From.String(), Yellow("->"), next.String()), zap.String("action", action.String()))
		switch next {
		case STATE_WAITPUBLISH:
			stateEvent = SEwaitPublish{event, r.Publisher}
			waitTime := time.Duration(0)
			if r.Publisher != nil {
				waitTime = r.Publisher.GetPublisher().Config.WaitCloseTimeout
				r.Tracks.Range(func(name string, t Track) {
					t.SetStuff(TrackStateOffline)
				})
			}
			r.Subscribers.OnPublisherLost(event)
			if suber := r.Subscribers.Pick(); suber != nil {
				r.Subscribers.Broadcast(stateEvent)
				if waitTime == 0 {
					waitTime = suber.GetSubscriber().Config.WaitTimeout
				}
			} else if waitTime == 0 {
				waitTime = time.Millisecond * 10 //没有订阅者也没有配置发布者等待重连时间，默认10ms后关闭流
			}
			r.timeout.Reset(waitTime)
			r.Debug("wait publisher", zap.Duration("wait timeout", waitTime))
		case STATE_PUBLISHING:
			if len(r.SEHistory) > 1 {
				stateEvent = SErepublish{event}
			} else {
				stateEvent = SEpublish{event}
			}
			r.Subscribers.Broadcast(stateEvent)
			// if r.IdleTimeout > 0 && r.Subscribers.Len() == 0 {
			// 	return r.action(ACTION_LASTLEAVE)
			// } else {
			r.timeout.Reset(r.PublishTimeout) // 5秒心跳，检测track的存活度
			// }
		case STATE_WAITCLOSE:
			stateEvent = SEwaitClose{event}
			if r.IdleTimeout > 0 {
				r.timeout.Reset(r.IdleTimeout)
			} else {
				r.timeout.Reset(r.DelayCloseTimeout)
			}
		case STATE_CLOSED:
			Streams.Delete(r.Path)
			r.timeout.Stop()
			stateEvent = SEclose{event}
			r.Subscribers.Broadcast(stateEvent)
			r.Tracks.Range(func(_ string, t Track) {
				t.Dispose()
			})
			r.Subscribers.Dispose()
			r.actionChan.Close()
		}
		EventBus <- stateEvent
		if r.Publisher != nil {
			r.Publisher.OnEvent(stateEvent)
		}
	} else {
		r.Debug("wrong action", zap.String("action", action.String()))
	}
	return
}

func (r *Stream) IsShutdown() bool {
	switch l := len(r.SEHistory); l {
	case 0:
		return false
	case 1:
		return r.SEHistory[0].Action == ACTION_CLOSE
	default:
		switch r.SEHistory[l-1].Action {
		case ACTION_CLOSE:
			return true
		case ACTION_TIMEOUT:
			return r.SEHistory[l-1].From == STATE_WAITCLOSE
		}
	}
	return false
}

func (r *Stream) IsClosed() bool {
	if r == nil {
		return true
	}
	return r.State == STATE_CLOSED
}

func (r *Stream) Close() {
	r.Receive(ACTION_CLOSE)
}

func (s *Stream) Receive(event any) bool {
	if s.IsClosed() {
		return false
	}
	return s.actionChan.Send(event)
}

func (s *Stream) onSuberClose(sub ISubscriber) {
	s.Subscribers.Delete(sub)
	if s.Publisher != nil {
		s.Publisher.OnEvent(sub) // 通知Publisher有订阅者离开，在回调中可以去获取订阅者数量
	}
	if (s.DelayCloseTimeout > 0 || s.IdleTimeout > 0) && s.Subscribers.Len() == 0 {
		s.action(ACTION_LASTLEAVE)
	}
}

func (s *Stream) checkRunCost(timeStart time.Time, timeOutInfo zap.Field) {
	if cost := time.Since(timeStart); cost > 100*time.Millisecond {
		s.Warn("run timeout", timeOutInfo, zap.Duration("cost", cost))
	}
}

// 流状态处理中枢，包括接收订阅发布指令等
func (s *Stream) run() {
	EventBus <- SEcreate{StreamEvent{Event[*Stream]{Target: s, Time: time.Now()}}}
	pulseTicker := time.NewTicker(EngineConfig.PulseInterval)
	defer pulseTicker.Stop()
	var timeOutInfo zap.Field
	var timeStart time.Time
	for pulseSuber := make(map[ISubscriber]struct{}); ; s.checkRunCost(timeStart, timeOutInfo) {
		select {
		case <-pulseTicker.C:
			timeStart = time.Now()
			timeOutInfo = zap.String("type", "pulse")
			for sub := range pulseSuber {
				sub.OnEvent(PulseEvent{CreateEvent(struct{}{})})
			}
		case <-s.timeout.C:
			timeStart = time.Now()
			timeOutInfo = zap.String("state", s.State.String())
			if s.State == STATE_PUBLISHING {
				for sub := range s.Subscribers.internal {
					if sub.IsClosed() {
						delete(s.Subscribers.internal, sub)
						s.Info("innersuber -1", zap.Int("remains", len(s.Subscribers.internal)))
					}
				}
				for sub := range s.Subscribers.public {
					if sub.IsClosed() {
						s.onSuberClose(sub)
					}
				}
				if !s.NeverTimeout {
					lost := false
					trackCount := 0
					timeout := s.PublishTimeout
					if s.IsPause {
						timeout = s.PauseTimeout
					}
					s.Tracks.Range(func(name string, t Track) {
						trackCount++
						switch t.(type) {
						case *track.Video, *track.Audio:
							// track 超过一定时间没有更新数据了
							if lastWriteTime := t.LastWriteTime(); !lastWriteTime.IsZero() && time.Since(lastWriteTime) > timeout {
								s.Warn("track timeout", zap.String("name", name), zap.Time("last writetime", lastWriteTime), zap.Duration("timeout", timeout))
								lost = true
							}
						}
					})
					if !lost {
						if trackCount == 0 {
							s.Warn("no tracks")
							lost = true
						} else if s.Publisher != nil && s.Publisher.IsClosed() {
							s.Warn("publish is closed")
							lost = true
						}
					}
					if lost {
						s.action(ACTION_PUBLISHLOST)
						continue
					}
					if s.IdleTimeout > 0 && s.Subscribers.Len() == 0 && time.Since(s.StartTime) > s.IdleTimeout {
						s.action(ACTION_LASTLEAVE)
						continue
					}
				}
				s.timeout.Reset(time.Second * 5)
				//订阅者等待音视频轨道超时了，放弃等待，订阅成功
				s.Subscribers.AbortWait()
			} else {
				s.Debug("timeout", timeOutInfo)
				s.action(ACTION_TIMEOUT)
			}
		case action, ok := <-s.actionChan.C:
			if !ok {
				return
			} else if s.State == STATE_CLOSED {
				if s.actionChan.Close() { //再次尝试关闭
					return
				}
				continue
			}
			timeStart = time.Now()
			switch v := action.(type) {
			case SubPulse:
				timeOutInfo = zap.String("action", "SubPulse")
				pulseSuber[v] = struct{}{}
			case *util.Promise[IPublisher]:
				timeOutInfo = zap.String("action", "Publish")
				if s.IsClosed() {
					v.Reject(ErrStreamIsClosed)
				}
				republish := s.Publisher == v.Value                                  // 重复发布
				kicked := !republish && s.Publisher != nil && s.Publisher.IsClosed() // 被踢下线
				if !republish {
					s.Publisher = v.Value
				}
				if s.action(ACTION_PUBLISH) || republish || kicked {
					v.Resolve()
					if s.Publisher.GetPublisher().Config.InsertSEI {
						if s.Tracks.SEI == nil {
							s.Tracks.SEI = track.NewDataTrack[[]byte]("sei")
							s.Tracks.SEI.Locker = &sync.Mutex{}
							s.Tracks.SEI.SetStuff(s)
							if s.Tracks.Add("sei", s.Tracks.SEI) {
								s.Info("sei track added")
							}
						}
					}
				} else {
					v.Reject(ErrDuplicatePublish)
				}
			case *util.Promise[ISubscriber]:
				timeOutInfo = zap.String("action", "Subscribe")
				if s.IsClosed() {
					v.Reject(ErrStreamIsClosed)
				}
				suber := v.Value
				io := suber.GetSubscriber()
				sbConfig := io.Config
				waits := &waitTracks{
					Promise: v,
				}
				if ats := io.Args.Get(sbConfig.SubAudioArgName); ats != "" {
					waits.audio.Wait(strings.Split(ats, ",")...)
				} else if len(sbConfig.SubAudioTracks) > 0 {
					waits.audio.Wait(sbConfig.SubAudioTracks...)
				} else if sbConfig.SubAudio {
					waits.audio.Wait()
				}
				if vts := io.Args.Get(sbConfig.SubVideoArgName); vts != "" {
					waits.video.Wait(strings.Split(vts, ",")...)
				} else if len(sbConfig.SubVideoTracks) > 0 {
					waits.video.Wait(sbConfig.SubVideoTracks...)
				} else if sbConfig.SubVideo {
					waits.video.Wait()
				}
				if dts := io.Args.Get(sbConfig.SubDataArgName); dts != "" {
					waits.data.Wait(strings.Split(dts, ",")...)
				} else {
					// waits.data.Wait()
				}
				if s.Publisher != nil {
					s.Publisher.OnEvent(v) // 通知Publisher有新的订阅者加入，在回调中可以去获取订阅者数量
					pubConfig := s.Publisher.GetPublisher().Config
					s.Tracks.Range(func(name string, t Track) {
						waits.Accept(t)
					})
					if !pubConfig.PubAudio || s.Subscribers.waitAborted {
						waits.audio.StopWait()
					}
					if !pubConfig.PubVideo || s.Subscribers.waitAborted {
						waits.video.StopWait()
					}
				}
				s.Subscribers.Add(suber, waits)
				if s.Subscribers.Len() == 1 && s.State == STATE_WAITCLOSE {
					s.action(ACTION_FIRSTENTER)
				}
			case Unsubscribe:
				timeOutInfo = zap.String("action", "Unsubscribe")
				delete(pulseSuber, v)
				s.onSuberClose(v)
			case TrackRemoved:
				timeOutInfo = zap.String("action", "TrackRemoved")
				name := v.GetName()
				if t, ok := s.Tracks.LoadAndDelete(name); ok {
					s.Info("track -1", zap.String("name", name))
					s.Subscribers.Broadcast(t)
					t.(common.Track).Dispose()
				}
			case *util.Promise[Track]:
				timeOutInfo = zap.String("action", "Track")
				if s.State == STATE_WAITPUBLISH {
					s.action(ACTION_PUBLISH)
				}
				pubConfig := s.GetPublisherConfig()
				name := v.Value.GetName()
				if _, ok := v.Value.(*track.Video); ok && !pubConfig.PubVideo {
					v.Reject(ErrTrackMute)
					continue
				}
				if _, ok := v.Value.(*track.Audio); ok && !pubConfig.PubAudio {
					v.Reject(ErrTrackMute)
					continue
				}
				if s.Tracks.Add(name, v.Value) {
					v.Resolve()
					s.Subscribers.OnTrack(v.Value)
					if _, ok := v.Value.(*track.Video); ok && !pubConfig.PubAudio {
						s.Subscribers.AbortWait()
					}
					if _, ok := v.Value.(*track.Audio); ok && !pubConfig.PubVideo {
						s.Subscribers.AbortWait()
					}
					// 这里重置的目的是当PublishTimeout设置很大的情况下，需要及时取消订阅者的等待
					s.timeout.Reset(time.Second * 5)
				} else {
					v.Reject(ErrBadTrackName)
				}
			case NoMoreTrack:
				s.Subscribers.AbortWait()
			case StreamAction:
				timeOutInfo = zap.String("action", "StreamAction"+v.String())
				s.action(v)
			default:
				timeOutInfo = zap.String("action", "unknown")
				s.Error("unknown action", timeOutInfo)
			}
		}
	}
}

func (s *Stream) AddTrack(t Track) (promise *util.Promise[Track]) {
	promise = util.NewPromise(t)
	s.Receive(promise)
	return
}

func (s *Stream) RemoveTrack(t Track) {
	s.Receive(TrackRemoved{t})
}

func (s *Stream) Pause() {
	s.IsPause = true
}

func (s *Stream) Resume() {
	s.IsPause = false
}

type TrackRemoved struct {
	Track
}

type SubPulse struct {
	ISubscriber
}

type Unsubscribe ISubscriber
type NoMoreTrack struct{}
