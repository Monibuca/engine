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
	SendHandler func(*avformat.SendPacket) error
	Cancel      context.CancelFunc
	Sign        string
	VTSent      bool
	ATSent      bool
	VSentTime   uint32
	ASentTime   uint32
	//packetQueue      chan *avformat.SendPacket
	dropCount  int
	OffsetTime uint32
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
	p := avformat.NewSendPacket(s.VideoTag, 0)
	s.SendHandler(p)
	p = avformat.NewSendPacket(s.AudioTag, 0)
	s.SendHandler(p)
	packet := s.FirstScreen
	s.VSentTime = packet.Timestamp
	s.ASentTime = packet.Timestamp
	for {
		select {
		case <-s.Done():
			return s.Err()
		default:
			packet.RLock()
			p = avformat.NewSendPacket(packet.AVPacket, packet.Timestamp-s.VSentTime)
			s.SendHandler(p)
			packet.RUnlock()
			packet = packet.next
		}
	}
}
