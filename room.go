package engine

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/Monibuca/engine/avformat"
	. "github.com/logrusorgru/aurora"
)

var (
	AllRoom   = RoomCollection{}
	roomCtxBg = context.Background()
)

// Collection 对sync.Map的包装
type RoomCollection struct {
	sync.Map
}

// Get 根据流名称获取房间
func (c *RoomCollection) Get(name string) (result *Room) {
	item, loaded := AllRoom.LoadOrStore(name, &Room{
		Subscribers:  make(map[string]*OutputStream),
		Control:      make(chan interface{}),
		AVRing:       NewRing(),
		WaitingMutex: new(sync.RWMutex),
		RoomInfo: RoomInfo{
			StreamPath:     name,
			SubscriberInfo: make([]*SubscriberInfo, 0),
		},
	})
	result = item.(*Room)
	if !loaded {
		result.Context, result.Cancel = context.WithCancel(roomCtxBg)
		result.WaitingMutex.Lock() //等待发布者
		go result.Run()
	}
	return
}

// Room 房间定义
type Room struct {
	context.Context
	Publisher
	RoomInfo     //可序列化，供后台查看的数据
	Control      chan interface{}
	Cancel       context.CancelFunc
	Subscribers  map[string]*OutputStream // 订阅者
	VideoTag     *avformat.AVPacket       // 每个视频包都是这样的结构,区别在于Payload的大小.FMS在发送AVC sequence header,需要加上 VideoTags,这个tag 1个字节(8bits)的数据
	AudioTag     *avformat.AVPacket       // 每个音频包都是这样的结构,区别在于Payload的大小.FMS在发送AAC sequence header,需要加上 AudioTags,这个tag 1个字节(8bits)的数据
	FirstScreen  *Ring                    //最近的关键帧位置，首屏渲染
	AVRing       *Ring                    //数据环
	WaitingMutex *sync.RWMutex            //用于订阅和等待发布者
	UseTimestamp bool                     //是否采用数据包中的时间戳
}

// RoomInfo 房间可序列化信息，用于控制台显示
type RoomInfo struct {
	StreamPath     string
	StartTime      time.Time
	SubscriberInfo []*SubscriberInfo
	Type           string
	VideoInfo      struct {
		PacketCount int
		CodecID     byte
		SPSInfo     avformat.SPSInfo
		BPS         int
		GOP         int //关键帧间隔
	}
	AudioInfo struct {
		PacketCount int
		SoundFormat byte //4bit
		SoundRate   int  //2bit
		SoundSize   byte //1bit
		SoundType   byte //1bit
		BPS         int
	}
}

// UnSubscribeCmd 取消订阅命令
type UnSubscribeCmd struct {
	*OutputStream
}

// SubscribeCmd 订阅房间命令
type SubscribeCmd struct {
	*OutputStream
}

// ChangeRoomCmd 切换房间命令
type ChangeRoomCmd struct {
	*OutputStream
	NewRoom *Room
}

func (r *Room) onClosed() {
	Print(Yellow("room destoryed :"), BrightCyan(r.StreamPath))
	AllRoom.Delete(r.StreamPath)
	OnRoomClosedHooks.Trigger(r)
	if r.Publisher != nil {
		r.OnClosed()
	}
}

//Subscribe 订阅房间
func (r *Room) Subscribe(s *OutputStream) {
	s.Room = r
	if r.Err() == nil {
		s.SubscribeTime = time.Now()
		Print(Sprintf(Yellow("subscribe :%s %s,to room %s"), Blue(s.Type), Cyan(s.ID), BrightCyan(r.StreamPath)))
		s.Context, s.Cancel = context.WithCancel(r)
		s.Control <- &SubscribeCmd{s}
	}
}

//UnSubscribe 取消订阅房间
func (r *Room) UnSubscribe(s *OutputStream) {
	if r.Err() == nil {
		r.Control <- &UnSubscribeCmd{s}
	}
}

// Run 房间运行，转发逻辑
func (r *Room) Run() {
	Print(Green("room create:"), BrightCyan(r.StreamPath))
	defer r.onClosed()
	for {
		select {
		case <-r.Done():
			return
		case s := <-r.Control:
			switch v := s.(type) {
			case *UnSubscribeCmd:
				if _, ok := r.Subscribers[v.ID]; ok {
					delete(r.Subscribers, v.ID)
					var hole int
					for i, val := range r.SubscriberInfo {
						if val == &v.SubscriberInfo {
							hole = i
							break
						}
					}
					copy(r.SubscriberInfo[hole:], r.SubscriberInfo[hole+1:])
					r.SubscriberInfo = r.SubscriberInfo[:len(r.SubscriberInfo)-1]
					OnUnSubscribeHooks.Trigger(v.OutputStream)
					Print(Sprintf(Yellow("%s subscriber %s removed remains:%d"), BrightCyan(r.StreamPath), Cyan(v.ID), Blue(len(r.SubscriberInfo))))
					if len(r.SubscriberInfo) == 0 && r.Publisher == nil {
						r.Cancel()
					}
				}
			case *SubscribeCmd:
				//防止重复添加
				if _, ok := r.Subscribers[v.ID]; !ok {
					r.Subscribers[v.ID] = v.OutputStream
					r.SubscriberInfo = append(r.SubscriberInfo, &v.SubscriberInfo)
					Print(Sprintf(Yellow("%s subscriber %s added remains:%d"), BrightCyan(r.StreamPath), Cyan(v.ID), Blue(len(r.SubscriberInfo))))
					OnSubscribeHooks.Trigger(v.OutputStream)
				}
			case *ChangeRoomCmd:
				if _, ok := v.NewRoom.Subscribers[v.ID]; !ok {
					delete(r.Subscribers, v.ID)
					v.NewRoom.Subscribe(v.OutputStream)
					if len(r.SubscriberInfo) == 0 && r.Publisher == nil {
						r.Cancel()
					}
				}
			}
		}
	}
}

// PushAudio 来自发布者推送的音频
func (r *Room) PushAudio(timestamp uint32, payload []byte) {
	payloadLen := len(payload)
	audio := r.AVRing
	audio.Type = avformat.FLV_TAG_TYPE_AUDIO
	audio.Timestamp = timestamp
	audio.Payload = payload
	audio.VideoFrameType = 0
	audio.IsSequence = false
	if audio.GetLast().Timestamp > 0 {
		r.AudioInfo.BPS = payloadLen * 1000 / int(timestamp-audio.GetLast().Timestamp)
	}
	if payloadLen < 4 {
		return
	}
	if payload[0] == 0xFF && (payload[1]&0xF0) == 0xF0 {
		//将ADTS转换成ASC
		r.AudioInfo.SoundFormat = 10
		r.AudioInfo.SoundRate = avformat.SamplingFrequencies[(payload[2]&0x3c)>>2]
		r.AudioInfo.SoundType = ((payload[2] & 0x1) << 2) | ((payload[3] & 0xc0) >> 6)
		r.AudioTag = audio.ADTS2ASC()
	} else if r.AudioTag == nil {
		audio.IsSequence = true
		if payloadLen < 5 {
			return
		}
		r.AudioTag = audio.AVPacket.Clone()
		tmp := payload[0]                                                      // 第一个字节保存着音频的相关信息
		if r.AudioInfo.SoundFormat = tmp >> 4; r.AudioInfo.SoundFormat == 10 { //真的是AAC的话，后面有一个字节的详细信息
			//0 = AAC sequence header，1 = AAC raw。
			if aacPacketType := payload[1]; aacPacketType == 0 {
				config1 := payload[2]
				config2 := payload[3]
				//audioObjectType = (config1 & 0xF8) >> 3
				// 1 AAC MAIN 	ISO/IEC 14496-3 subpart 4
				// 2 AAC LC 	ISO/IEC 14496-3 subpart 4
				// 3 AAC SSR 	ISO/IEC 14496-3 subpart 4
				// 4 AAC LTP 	ISO/IEC 14496-3 subpart 4
				r.AudioInfo.SoundRate = avformat.SamplingFrequencies[((config1&0x7)<<1)|(config2>>7)]
				r.AudioInfo.SoundType = (config2 >> 3) & 0x0F //声道
				//frameLengthFlag = (config2 >> 2) & 0x01
				//dependsOnCoreCoder = (config2 >> 1) & 0x01
				//extensionFlag = config2 & 0x01
			}
		} else {
			r.AudioInfo.SoundRate = avformat.SoundRate[(tmp&0x0c)>>2] // 采样率 0 = 5.5 kHz or 1 = 11 kHz or 2 = 22 kHz or 3 = 44 kHz
			r.AudioInfo.SoundSize = (tmp & 0x02) >> 1                 // 采样精度 0 = 8-bit samples or 1 = 16-bit samples
			r.AudioInfo.SoundType = tmp & 0x01                        // 0 单声道，1立体声
		}
		return
	}
	if !r.UseTimestamp {
		audio.Timestamp = uint32(time.Since(r.StartTime) / time.Millisecond)
	}
	r.AudioInfo.PacketCount++
	audio.Number = r.AudioInfo.PacketCount
	audio.GoNext()
	audio.Lock()
	audio.GetLast().Unlock()
}
func (r *Room) setH264Info(video *Ring) {
	r.VideoTag = video.AVPacket.Clone()
	if r.VideoInfo.CodecID != 7 {
		return
	}
	info := avformat.AVCDecoderConfigurationRecord{}
	//0:codec,1:IsAVCSequence,2~4:compositionTime
	if _, err := info.Unmarshal(video.Payload[5:]); err == nil {
		r.VideoInfo.SPSInfo, err = avformat.ParseSPS(info.SequenceParameterSetNALUnit)
	}
}

// PushVideo 来自发布者推送的视频
func (r *Room) PushVideo(timestamp uint32, payload []byte) {
	payloadLen := len(payload)
	video := r.AVRing
	video.Type = avformat.FLV_TAG_TYPE_VIDEO
	video.Timestamp = timestamp
	video.Payload = payload
	if video.GetLast().Timestamp > 0 {
		r.VideoInfo.BPS = payloadLen * 1000 / int(timestamp-video.GetLast().Timestamp)
	}
	if payloadLen < 3 {
		return
	}
	video.VideoFrameType = payload[0] >> 4  // 帧类型 4Bit, H264一般为1或者2
	r.VideoInfo.CodecID = payload[0] & 0x0f // 编码类型ID 4Bit, JPEG, H263, AVC...
	video.IsSequence = video.VideoFrameType == 1 && payload[1] == 0
	if r.VideoTag == nil {
		if video.IsSequence {
			r.setH264Info(video)
		} else {
			log.Println("no AVCSequence")
		}
	} else {
		//更换AVCSequence
		if video.IsSequence {
			r.setH264Info(video)
		}
		if video.IsKeyFrame() {
			if r.FirstScreen == nil {
				defer r.WaitingMutex.Unlock()
				r.FirstScreen = video.Clone()
			} else {
				oldNumber := r.FirstScreen.Number
				r.FirstScreen.Index = video.Index
				r.FirstScreen.RingItem = video.RingItem
				r.VideoInfo.GOP = r.FirstScreen.Number - oldNumber
			}
		}
		if !r.UseTimestamp {
			video.Timestamp = uint32(time.Since(r.StartTime) / time.Millisecond)
		}
		r.VideoInfo.PacketCount++
		video.Number = r.VideoInfo.PacketCount
		video.GoNext()
		video.Lock()
		video.GetLast().Unlock()
	}
}
