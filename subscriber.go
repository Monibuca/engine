package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/Monibuca/engine/avformat"
	"github.com/pkg/errors"
)

// Subscriber 订阅者
type Subscriber interface {
	Send(*avformat.SendPacket) error
}

// SubscriberInfo 订阅者可序列化信息，用于控制台输出
type SubscriberInfo struct {
	ID            string
	TotalDrop     int //总丢帧
	TotalPacket   int
	Type          string
	BufferLength  int
	SubscribeTime time.Time
}

// OutputStream 订阅者实体定义
type OutputStream struct {
	context.Context
	*Room
	SubscriberInfo
	SendHandler func(uint32, *avformat.AVPacket) error
	Cancel      context.CancelFunc
	Sign        string
	dropCount   int
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

	s.SendHandler(0, s.VideoTag)
	packet := s.FirstScreen
	startTime := packet.Timestamp
	s.SendHandler(0, packet.AVPacket)
	packet = packet.next
	s.SendHandler(0, s.AudioTag)
	for {
		select {
		case <-s.Done():
			return s.Err()
		default:
			packet.RLock()
			s.SendHandler(packet.Timestamp-startTime, packet.AVPacket)
			packet.RUnlock()
			packet = s.checkDrop(packet)
		}
	}
}
func (s *OutputStream) checkDrop(packet *CircleItem) *CircleItem {
	pIndex := s.AVCircle.index
	if pIndex < packet.index {
		pIndex = pIndex + CIRCLE_SIZE
	}
	if pIndex-packet.index > CIRCLE_SIZE/2 {
		droped := 0
		for packet = packet.next; !packet.IsKeyFrame(); packet = packet.next {
			droped++
		}
		fmt.Println("drop package ", droped)
		s.dropCount += droped
		return packet
	}
	return packet.next
}
