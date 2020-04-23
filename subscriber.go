package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/Monibuca/engine/avformat"
	"github.com/pkg/errors"
)

// Subscriber 订阅者
// type Subscriber interface {
// 	Send(*avformat.SendPacket) error
// }

// SubscriberInfo 订阅者可序列化信息，用于控制台输出
type SubscriberInfo struct {
	ID            string
	TotalDrop     int //总丢帧
	TotalPacket   int
	Type          string
	BufferLength  int
	Delay         uint32
	SubscribeTime time.Time
}

// OutputStream 订阅者实体定义
type OutputStream struct {
	context.Context
	*Room
	SubscriberInfo
	SendHandler func(*avformat.SendPacket) error
	Cancel      context.CancelFunc
	Sign        string
	OffsetTime  uint32
}

// IsClosed 检查订阅者是否已经关闭
func (s *OutputStream) IsClosed() bool {
	return s.Context != nil && s.Err() != nil
}

// Close 关闭订阅者
func (s *OutputStream) Close() {
	if s.Cancel != nil {
		s.Cancel()
	}
}

//Play 开始订阅
func (s *OutputStream) Play(streamPath string) (err error) {
	if !config.EnableWaitRoom {
		if _, ok := AllRoom.Load(streamPath); !ok {
			return errors.New(fmt.Sprintf("Stream not found:%s", streamPath))
		}
	}
	AllRoom.Get(streamPath).Subscribe(s)
	defer s.UnSubscribe(s)
	s.WaitingMutex.RLock()
	s.WaitingMutex.RUnlock()
	sendPacket := avformat.NewSendPacket(s.VideoTag, 0)
	defer sendPacket.Recycle()
	s.SendHandler(sendPacket)
	packet := s.FirstScreen
	startTime := packet.Timestamp
	packet.RLock()
	sendPacket.AVPacket = packet.AVPacket
	s.SendHandler(sendPacket)
	packet.RUnlock()
	packet = packet.next
	atsent := false
	dropping := false
	droped := 0
	for {
		select {
		case <-s.Done():
			return s.Err()
		default:
			s.TotalPacket++
			packet.RLock()
			if !dropping {
				if packet.Type == avformat.FLV_TAG_TYPE_AUDIO && !atsent {
					sendPacket.AVPacket = s.AudioTag
					sendPacket.Timestamp = 0
					s.SendHandler(sendPacket)
					atsent = true
				}
				sendPacket.AVPacket = packet.AVPacket
				sendPacket.Timestamp = packet.Timestamp - startTime
				s.SendHandler(sendPacket)
				if s.checkDrop(packet) {
					dropping = true
					droped = 0
				}
				packet.RUnlock()
				packet = packet.next
			} else if packet.AVPacket.IsKeyFrame() {
				dropping = false
				//fmt.Println("drop package ", droped)
				s.TotalDrop += droped
				packet.RUnlock()
			} else {
				droped++
				packet.RUnlock()
				packet = packet.next
			}
		}
	}
}
func (s *OutputStream) checkDrop(packet *CircleItem) bool {
	pIndex := s.AVCircle.index
	if pIndex < packet.index {
		pIndex = pIndex + CIRCLE_SIZE
	}
	s.BufferLength = pIndex - packet.index
	s.Delay = s.AVCircle.Timestamp - packet.Timestamp
	return s.BufferLength > CIRCLE_SIZE/2
}
