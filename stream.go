package engine

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"
	"unsafe"

	. "github.com/logrusorgru/aurora"
	"go.uber.org/zap"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/config"
	"m7s.live/engine/v4/log"
	"m7s.live/engine/v4/track"
	"m7s.live/engine/v4/util"
)

type StreamState byte
type StreamAction byte

type StateEvent struct {
	Action StreamAction
	From   StreamState
	Stream *Stream `json:"-"`
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

type SErepublish struct {
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
type UnsubscribeEvent struct {
	Subscriber ISubscriber
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

type StreamList []*Stream

func (l StreamList) Len() int {
	return len(l)
}

func (l StreamList) Less(i, j int) bool {
	return l[i].Path < l[j].Path
}

func (l StreamList) Swap(i, j int) {
	l[i], l[j] = l[j], l[i]
}

func (l StreamList) Sort() {
	sort.Sort(l)
}

func GetSortedStreamList() StreamList {
	result := StreamList(Streams.ToList())
	result.Sort()
	return result
}

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
	PublishTimeout    time.Duration //发布者无数据后超时
	DelayCloseTimeout time.Duration //发布者丢失后等待
}
type Tracks struct {
	util.Map[string, Track]
	MainVideo *track.Video
}

func (tracks *Tracks) Add(name string, t Track) bool {
	switch v := t.(type) {
	case *track.Video:
		if tracks.MainVideo == nil {
			tracks.MainVideo = v
			tracks.SetIDR(v)
		}
	case *track.Audio:
		if tracks.MainVideo != nil {
			v.Narrow()
		}
	}
	return tracks.Map.Add(name, t)
}

func (tracks *Tracks) SetIDR(video Track) {
	if video == tracks.MainVideo {
		tracks.Map.Range(func(_ string, t Track) {
			if v, ok := t.(*track.Audio); ok {
				v.Narrow()
			}
		})
	}
}

func (tracks *Tracks) MarshalJSON() ([]byte, error) {
	return json.Marshal(util.MapList(&tracks.Map, func(_ string, t Track) Track {
		t.SnapForJson()
		return t
	}))
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
	SEHistory   []StateEvent // 事件历史
	Subscribers Subscribers  // 订阅者
	Tracks      Tracks
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

func (s *Stream) GetStartTime() time.Time {
	return s.StartTime
}

func (s *Stream) GetPublisherConfig() *config.Publish {
	return s.Publisher.GetPublisher().Config
}

// Summary 返回流的简要信息
func (s *Stream) Summary() (r StreamSummay) {
	if s.Publisher != nil {
		r.Type = s.Publisher.GetPublisher().Type
	}
	r.Tracks = util.MapList(&s.Tracks.Map, func(name string, t Track) string {
		b := t.GetBase()
		r.BPS += b.BPS
		return name
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
	if s, ok := Streams.Map[streamPath]; ok {
		s.Debug("Stream Found")
		return s, false
	} else {
		p := strings.Split(streamPath, "/")
		s = &Stream{
			Path:       streamPath,
			AppName:    p[0],
			StreamName: util.LastElement(p),
			StartTime:  time.Now(),
			timeout:    time.NewTimer(waitTimeout),
		}
		s.Subscribers.Init()
		s.Logger = log.With(zap.String("stream", streamPath))
		s.Info("created")
		Streams.Map[streamPath] = s
		s.actionChan.Init(1)
		s.Tracks.Init()
		go s.run()
		return s, true
	}
}

func (r *Stream) action(action StreamAction) (ok bool) {
	event := StateEvent{action, r.State, r}
	var next StreamState
	if next, ok = event.Next(); ok {
		r.State = next
		r.SEHistory = append(r.SEHistory, event)
		// 给Publisher状态变更的回调，方便进行远程拉流等操作
		var stateEvent any
		r.Info(Sprintf("%s%s%s", StateNames[event.From], Yellow("->"), StateNames[next]), zap.String("action", ActionNames[action]))
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
		case STATE_PUBLISHING:
			if len(r.SEHistory) > 1 {
				stateEvent = SErepublish{event}
			} else {
				stateEvent = SEpublish{event}
			}
			r.Subscribers.Broadcast(stateEvent)
			r.timeout.Reset(r.PublishTimeout) // 5秒心跳，检测track的存活度
		case STATE_WAITCLOSE:
			stateEvent = SEwaitClose{event}
			r.timeout.Reset(r.DelayCloseTimeout)
		case STATE_CLOSED:
			for !r.actionChan.Close() {
				// 等待channel发送完毕，伪自旋锁
				time.Sleep(time.Millisecond * 100)
			}
			stateEvent = SEclose{event}
			r.Subscribers.Broadcast(stateEvent)
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
	if s.DelayCloseTimeout > 0 && s.Subscribers.Len() == 0 {
		s.action(ACTION_LASTLEAVE)
	}
}

// 流状态处理中枢，包括接收订阅发布指令等
func (s *Stream) run() {
	for {
		select {
		case <-s.timeout.C:
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
				s.Tracks.ModifyRange(func(name string, t Track) {
					// track 超过一定时间没有更新数据了
					if lastWriteTime := t.LastWriteTime(); !lastWriteTime.IsZero() && time.Since(lastWriteTime) > s.PublishTimeout {
						s.Warn("track timeout", zap.String("name", name), zap.Time("lastWriteTime", lastWriteTime), zap.Duration("timeout", s.PublishTimeout))
						delete(s.Tracks.Map.Map, name)
						s.Subscribers.Broadcast(TrackRemoved{t})
					}
				})
				if s.State != STATE_PUBLISHING {
					continue
				}
				if s.Tracks.Len() == 0 || (s.Publisher != nil && s.Publisher.IsClosed()) {
					s.action(ACTION_PUBLISHLOST)
				} else {
					s.timeout.Reset(time.Second * 5)
					//订阅者等待音视频轨道超时了，放弃等待，订阅成功
					s.Subscribers.AbortWait()
				}
			} else {
				s.Debug("timeout", zap.String("state", StateNames[s.State]))
				s.action(ACTION_TIMEOUT)
			}
		case action, ok := <-s.actionChan.C:
			if ok {
				switch v := action.(type) {
				case *util.Promise[IPublisher]:
					if s.IsClosed() {
						v.Reject(ErrStreamIsClosed)
					}
					republish := s.Publisher == v.Value // 重复发布
					if !republish {
						s.Publisher = v.Value
					}
					if s.action(ACTION_PUBLISH) || republish {
						v.Resolve()
					} else {
						v.Reject(ErrBadStreamName)
					}
				case *util.Promise[ISubscriber]:
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
				case ISubscriber:
					s.onSuberClose(v)
				case TrackRemoved:
					name := v.GetBase().Name
					if t, ok := s.Tracks.Delete(name); ok {
						s.Info("track -1", zap.String("name", name))
						s.Subscribers.Broadcast(t)
						if s.Tracks.Len() == 0 {
							s.action(ACTION_PUBLISHLOST)
						}
						if dt, ok := t.(*track.Data); ok {
							dt.Dispose()
						}
					}
				case *util.Promise[Track]:
					if s.State == STATE_WAITPUBLISH {
						s.action(ACTION_PUBLISH)
					}
					name := v.Value.GetBase().Name
					if s.Tracks.Add(name, v.Value) {
						v.Resolve()
						s.Subscribers.OnTrack(v.Value)
					} else {
						v.Reject(ErrBadTrackName)
					}
				case StreamAction:
					s.action(v)
				default:
					s.Error("unknown action", zap.Any("action", action))
				}
			} else {
				s.Subscribers.Dispose()
				s.Tracks.Range(func(_ string, t Track) {
					if dt, ok := t.(*track.Data); ok {
						dt.Dispose()
					}
				})
				return
			}
		}
	}
}

func (s *Stream) AddTrack(t *util.Promise[Track]) {
	s.Receive(t)
}

type TrackRemoved struct {
	Track
}

func (s *Stream) RemoveTrack(t Track) {
	s.Receive(TrackRemoved{t})
}

func (r *Stream) NewDataTrack(name string, locker sync.Locker) (dt *track.Data) {
	dt = &track.Data{
		Locker: locker,
	}
	dt.Init(10)
	dt.SetStuff(name, r)
	return
}
